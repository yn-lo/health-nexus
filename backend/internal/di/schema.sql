-- ============================================================================
-- schema.sql — 数据库结构的单一事实来源（幂等）
-- ----------------------------------------------------------------------------
-- 用法：由应用启动时（internal/di.ApplySchema）按内容哈希幂等应用：
--   · 哈希未变化 → 跳过；
--   · 哈希变化 → 整文件重跑（本文件全部 DDL 均为幂等/可安全重放形式）。
--
-- ⚠️ 维护约定（务必遵守）：
--   1. 所有对象必须用 IF NOT EXISTS / OR REPLACE 或 DO 块守卫写成幂等；
--   2. 新增表/列用 CREATE TABLE IF NOT EXISTS / ADD COLUMN IF NOT EXISTS；
--   3. 破坏性变更（DROP/改类型）写成"幂等且不破坏已有列"（如 DROP COLUMN IF EXISTS）；
--   4. 尽量避免破坏性变更；确需时确认对已有环境重跑安全。
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================================
-- base 域
-- ============================================================================

CREATE TABLE IF NOT EXISTS departments (
    id           BIGSERIAL    PRIMARY KEY,
    name         VARCHAR(100) NOT NULL,
    parent_id    BIGINT       REFERENCES departments(id) ON DELETE RESTRICT,
    is_public    BOOLEAN      NOT NULL DEFAULT FALSE,
    is_active    BOOLEAN      NOT NULL DEFAULT TRUE,
    description  TEXT         NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_departments (
    id            BIGSERIAL   PRIMARY KEY,
    user_id       BIGINT      NOT NULL,
    department_id BIGINT      NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    is_primary    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, department_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_user_departments_one_primary
    ON user_departments(user_id) WHERE is_primary = TRUE;
CREATE INDEX IF NOT EXISTS idx_departments_parent_id   ON departments (parent_id);
CREATE INDEX IF NOT EXISTS idx_user_departments_user_id ON user_departments (user_id);
CREATE INDEX IF NOT EXISTS idx_user_departments_dept_id ON user_departments (department_id);

-- ============================================================================
-- auth 域
-- ============================================================================

CREATE TABLE IF NOT EXISTS users (
    id                 BIGSERIAL    PRIMARY KEY,
    username           VARCHAR(64)  NOT NULL UNIQUE,
    role               VARCHAR(20)  NOT NULL,
    password_hash      VARCHAR(255) NOT NULL DEFAULT '',
    phone              VARCHAR(20)  NOT NULL DEFAULT '',
    date_of_birth      DATE,
    gender             VARCHAR(10)  NOT NULL DEFAULT '',
    emergency_contact  VARCHAR(64)  NOT NULL DEFAULT '',
    emergency_phone    VARCHAR(20)  NOT NULL DEFAULT '',
    is_active          BOOLEAN      NOT NULL DEFAULT TRUE,
    is_deleted         BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT users_role_chk CHECK (role IN ('SUPER_ADMIN','DEPT_ADMIN','DOCTOR','NURSE','PATIENT'))
);

CREATE INDEX IF NOT EXISTS idx_users_is_deleted ON users (is_deleted) WHERE is_deleted = FALSE;

-- ============================================================================
-- wiki 域
-- ============================================================================

CREATE TABLE IF NOT EXISTS articles (
    id                 BIGSERIAL    PRIMARY KEY,
    title              VARCHAR(255) NOT NULL,
    content            TEXT         NOT NULL,
    summary            TEXT         NOT NULL DEFAULT '',
    cover_image_url    TEXT         NOT NULL DEFAULT '',
    status             VARCHAR(20)  NOT NULL DEFAULT 'draft',
    version            INT          NOT NULL DEFAULT 1,
    content_hash       CHAR(64)     NOT NULL DEFAULT '',
    author_id          BIGINT       NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    department_id      BIGINT       REFERENCES departments(id) ON DELETE SET NULL,
    reviewer_id        BIGINT,
    review_comment     TEXT         NOT NULL DEFAULT '',
    view_count         BIGINT       NOT NULL DEFAULT 0,
    is_deleted         BOOLEAN      NOT NULL DEFAULT FALSE,
    allow_reference    BOOLEAN      NOT NULL DEFAULT FALSE,
    review_overdue     BOOLEAN      NOT NULL DEFAULT FALSE,
    review_overdue_at  TIMESTAMPTZ,
    published_at       TIMESTAMPTZ,
    featured_rank      INT          NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT articles_status_chk CHECK (status IN ('draft','pending','published','archived','deleted')),
    CONSTRAINT articles_featured_rank_check CHECK (featured_rank BETWEEN 0 AND 3)
);

-- P1 修复：知识条目的来源、适用人群、有效期与内容风险等级。
-- 逾期策略按内容风险区分（高风险资料有效期更短），检索排除"高风险且已逾期"的文章。
ALTER TABLE articles ADD COLUMN IF NOT EXISTS source VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE articles ADD COLUMN IF NOT EXISTS applicable_population VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE articles ADD COLUMN IF NOT EXISTS valid_until TIMESTAMPTZ;
ALTER TABLE articles ADD COLUMN IF NOT EXISTS content_risk VARCHAR(20) NOT NULL DEFAULT 'normal';
ALTER TABLE articles DROP CONSTRAINT IF EXISTS articles_content_risk_chk;
ALTER TABLE articles ADD CONSTRAINT articles_content_risk_chk CHECK (content_risk IN ('normal','high'));

-- P1 修复：分离「编辑稿」与「已发布快照」。
-- content 始终是编辑稿（作者最新提交，可能是未审核内容）；published_content 是最近一次
-- 审核通过的正文快照。患者详情与 RAG 检索一律读 published_content——
-- 否则已发布文章被编辑后（status=pending），待审核的新正文会直接对患者公开（绕过审核）。
--
-- 存量回填**仅限当前确为 published 的文章**：status='published' 表示当前 content 就是
-- 审核通过的正文。pending（已发布文章被编辑、待重新审核）的 content 已是**未审核的新稿**，
-- 其 published_at 仅代表"历史上发布过"，此时把 content 回填为快照 = 直接公开未审核草稿（P1）。
-- pending 的旧审核版本在本迁移前已被覆盖、无法从本表还原，故不做回填：
-- 快照保持空串 → 患者详情/检索读不到内容（安全失败），由管理员重新审核发布后建立快照。
ALTER TABLE articles ADD COLUMN IF NOT EXISTS published_content TEXT NOT NULL DEFAULT '';
ALTER TABLE articles ADD COLUMN IF NOT EXISTS published_content_hash CHAR(64) NOT NULL DEFAULT '';
-- published_version：快照对应的文章版本号。切片携带写入时的 version，
-- 检索据此只命中"与审核版本一致的切片"，避免并发重建产生的超前版本切片被检索命中（P1 绑定审核版本）。
ALTER TABLE articles ADD COLUMN IF NOT EXISTS published_version INT NOT NULL DEFAULT 0;
UPDATE articles
   SET published_content = content, published_content_hash = content_hash, published_version = version
 WHERE status = 'published' AND published_content = '' AND content <> '';

-- 污染修复：修正此前按"published_at 非空"误回填的 pending 文章快照。
-- 这些文章的 published_content 是未审核的新稿，必须清空（宁可暂时不可读，也不公开未审核内容）。
-- 幂等：仅在快照与当前编辑稿一致（即确为误回填）时清空，已重新审核发布的文章不受影响。
UPDATE articles
   SET published_content = '', published_content_hash = '', published_version = 0
 WHERE status = 'pending'
   AND published_content <> ''
   AND published_content = content;

CREATE TABLE IF NOT EXISTS article_chunks (
    id            BIGSERIAL   PRIMARY KEY,
    article_id    BIGINT      NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    chunk_index   INT         NOT NULL,
    content       TEXT        NOT NULL,
    content_hash  CHAR(64)    NOT NULL DEFAULT '',
    embedding     vector(1024),
    is_active     BOOLEAN     NOT NULL DEFAULT TRUE,
    version       INT         NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- P0 修复：记录向量所属 Embedding 模型。切换模型后旧向量不可比，检索按模型过滤，
-- 空串表示迁移前的历史切片（模型未知），由 outbox relay 全量重建后收敛为严格同模型。
ALTER TABLE article_chunks ADD COLUMN IF NOT EXISTS embedding_model VARCHAR(128) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_article_chunks_model ON article_chunks (embedding_model) WHERE is_active = true;

-- 对已有库的增量同步：移除已废弃的 BM25 全文检索链路（纯向量检索，幂等）。
DROP TRIGGER IF EXISTS trg_article_chunks_tsv ON article_chunks;
ALTER TABLE article_chunks DROP COLUMN IF EXISTS tsv;
DROP INDEX IF EXISTS idx_article_chunks_tsv;
DROP FUNCTION IF EXISTS article_chunks_tsv_update();
DROP FUNCTION IF EXISTS bigram_tsvector(text);
DROP FUNCTION IF EXISTS bigram_tsquery(text);
DROP FUNCTION IF EXISTS bigram_array(text);

CREATE INDEX IF NOT EXISTS idx_article_chunks_embedding
    ON article_chunks USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
CREATE INDEX IF NOT EXISTS idx_article_chunks_article ON article_chunks (article_id);
CREATE INDEX IF NOT EXISTS idx_article_chunks_active  ON article_chunks (article_id, is_active);

CREATE TABLE IF NOT EXISTS article_references (
    id              BIGSERIAL   PRIMARY KEY,
    article_id      BIGINT      NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    source_dept_id  BIGINT      NOT NULL REFERENCES departments(id) ON DELETE RESTRICT,
    target_dept_id  BIGINT      NOT NULL REFERENCES departments(id) ON DELETE RESTRICT,
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    applicant_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reviewer_id     BIGINT,
    review_comment  TEXT        NOT NULL DEFAULT '',
    approved_at     TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT article_refs_status_chk CHECK (status IN ('pending','approved','rejected','revoked'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_article_refs_pending
    ON article_references (article_id, target_dept_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_article_refs_source ON article_references (source_dept_id);
CREATE INDEX IF NOT EXISTS idx_article_refs_target ON article_references (target_dept_id);
CREATE INDEX IF NOT EXISTS idx_article_refs_status ON article_references (status);

CREATE TABLE IF NOT EXISTS article_audit_logs (
    id            BIGSERIAL   PRIMARY KEY,
    article_id    BIGINT      NOT NULL REFERENCES articles(id) ON DELETE RESTRICT,
    operator_id   BIGINT      NOT NULL,
    action        VARCHAR(50) NOT NULL,
    from_status   VARCHAR(20) NOT NULL DEFAULT '',
    to_status     VARCHAR(20) NOT NULL DEFAULT '',
    summary       TEXT        NOT NULL DEFAULT '',
    reason        TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_articles_status_dept    ON articles (status, department_id) WHERE is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_articles_author         ON articles (author_id);
CREATE INDEX IF NOT EXISTS idx_articles_published_at   ON articles (published_at);
CREATE INDEX IF NOT EXISTS idx_articles_review_overdue ON articles (review_overdue_at) WHERE review_overdue = FALSE;
CREATE INDEX IF NOT EXISTS idx_article_audit_logs_article ON article_audit_logs (article_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_articles_featured_rank
    ON articles (department_id, featured_rank) WHERE featured_rank > 0;
CREATE INDEX IF NOT EXISTS idx_articles_featured
    ON articles (department_id, view_count DESC, published_at DESC)
    WHERE status = 'published' AND is_deleted = false;

-- ============================================================================
-- chat 域
-- ============================================================================

CREATE TABLE IF NOT EXISTS conversations (
    id              UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    patient_id      BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    locked_dept_id  BIGINT       REFERENCES departments(id) ON DELETE SET NULL,
    title           VARCHAR(255) NOT NULL DEFAULT '',
    is_archived     BOOLEAN      NOT NULL DEFAULT FALSE,
    -- 首条消息落库时才置值（会话列表据此隐藏"创建后未成功产生任何消息"的空会话）。
    last_message_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 兼容既有库：last_message_at 原为 NOT NULL DEFAULT now()，导致空会话无法与有消息会话区分。
ALTER TABLE conversations ALTER COLUMN last_message_at DROP NOT NULL;
ALTER TABLE conversations ALTER COLUMN last_message_at DROP DEFAULT;

CREATE INDEX IF NOT EXISTS idx_conversations_patient_archived
    ON conversations (patient_id, is_archived);
CREATE INDEX IF NOT EXISTS idx_conversations_patient_archived_lastmsg
    ON conversations (patient_id, is_archived, last_message_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversations_locked_dept ON conversations (locked_dept_id);

CREATE TABLE IF NOT EXISTS messages (
    id                UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    conversation_id   UUID         NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    -- 本轮生成标识：同一轮的 user / assistant 消息共享（历史数据为 NULL）。
    turn_id           UUID,
    role              VARCHAR(20)  NOT NULL,
    content           TEXT         NOT NULL,
    result_code       VARCHAR(20)  NOT NULL DEFAULT '',
    referenced_chunks JSONB        NOT NULL DEFAULT '[]'::jsonb,
    feedback          VARCHAR(10)  DEFAULT NULL,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT messages_role_chk CHECK (role IN ('user','assistant')),
    CONSTRAINT messages_result_chk CHECK (result_code IN ('','ANSWERED','PARTIAL','REJECTED','INTERCEPTED','CRISIS','RATE_LIMITED')),
    CONSTRAINT messages_feedback_chk CHECK (feedback IS NULL OR feedback IN ('solved', 'partial', 'unsolved'))
);

-- 兼容既有库：老表补 turn_id 列。
ALTER TABLE messages ADD COLUMN IF NOT EXISTS turn_id UUID;

-- 反馈三态迁移（宣教效果口径）：up/down → solved/partial/unsolved。
-- 老口径值重置为 NULL（不参与新口径统计）；约束以 DROP+ADD 幂等重建。
UPDATE messages SET feedback = NULL
WHERE feedback IS NOT NULL AND feedback NOT IN ('solved', 'partial', 'unsolved');
ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_feedback_chk;
ALTER TABLE messages ADD CONSTRAINT messages_feedback_chk
    CHECK (feedback IS NULL OR feedback IN ('solved', 'partial', 'unsolved'));

-- P1 修复：同轮 user/assistant 消息在同一事务内以 now()（事务开始时间）落库，created_at 完全相同，
-- 随机 UUID 无法稳定定序，列表/历史上下文可能出现"答案在问题前"。
-- 引入全局单调递增 seq（全局单调 ⇒ 会话内单调；同一事务内 INSERT 顺序即 seq 顺序：
-- 先 user 后 assistant）。新消息由默认值 nextval 填充，老数据按 (conversation_id, created_at, id) 回填。
CREATE SEQUENCE IF NOT EXISTS messages_seq;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS seq BIGINT;
-- 回填历史 NULL 行（幂等：仅处理 NULL 行；按会话内时间与 id 稳定排序）。
UPDATE messages SET seq = numbered.rn FROM (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY conversation_id ORDER BY created_at ASC, id ASC) AS rn
    FROM messages WHERE seq IS NULL
) numbered WHERE messages.id = numbered.id;
ALTER TABLE messages ALTER COLUMN seq SET DEFAULT nextval('messages_seq');
ALTER TABLE messages ALTER COLUMN seq SET NOT NULL;
-- 序列水位对齐到现有最大值（幂等）：保证 nextval 新值不与回填值冲突。
SELECT setval('messages_seq', GREATEST((SELECT COALESCE(MAX(seq), 0) FROM messages), 1));

CREATE INDEX IF NOT EXISTS idx_messages_conv_created ON messages (conversation_id, created_at);
-- 消息列表/历史上下文统一按 (conversation_id, seq) 定序。
CREATE INDEX IF NOT EXISTS idx_messages_conv_seq ON messages (conversation_id, seq);
-- 幂等重放按轮定位结果（turn_id 唯一标识一轮）。
CREATE INDEX IF NOT EXISTS idx_messages_turn ON messages (turn_id);

-- 回填（幂等）：把历史遗留的"无任何消息"会话置空，使其不再出现在会话列表。
UPDATE conversations SET last_message_at = NULL
WHERE last_message_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM messages WHERE messages.conversation_id = conversations.id);

CREATE TABLE IF NOT EXISTS crisis_events (
    id                BIGSERIAL   PRIMARY KEY,
    patient_id        BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    conversation_id   UUID        NOT NULL REFERENCES conversations(id) ON DELETE RESTRICT,
    message_id        UUID        REFERENCES messages(id) ON DELETE SET NULL,
    triggered_content TEXT        NOT NULL DEFAULT '',
    matched_keywords  TEXT[]      NOT NULL DEFAULT '{}',
    level             VARCHAR(20) NOT NULL DEFAULT 'medium',
    is_handled        BOOLEAN     NOT NULL DEFAULT FALSE,
    handler_id        BIGINT,
    handled_at        TIMESTAMPTZ,
    handle_note       TEXT        NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT crisis_level_chk CHECK (level IN ('high','medium','low'))
);

CREATE INDEX IF NOT EXISTS idx_crisis_events_patient       ON crisis_events (patient_id);
CREATE INDEX IF NOT EXISTS idx_crisis_events_handled       ON crisis_events (is_handled);
CREATE INDEX IF NOT EXISTS idx_crisis_events_level         ON crisis_events (level);
CREATE INDEX IF NOT EXISTS idx_crisis_events_created       ON crisis_events (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_crisis_events_handled_level ON crisis_events (is_handled, level);

-- P1 人工闭环：接单响应时限与超时升级（未在时限内被处理的事件升级到超管）。
ALTER TABLE crisis_events ADD COLUMN IF NOT EXISTS acknowledge_due_at TIMESTAMPTZ;
ALTER TABLE crisis_events ADD COLUMN IF NOT EXISTS escalated_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_crisis_events_pending_due
    ON crisis_events (acknowledge_due_at) WHERE is_handled = false AND escalated_at IS NULL;

-- P1 危机通知 outbox：与向量化 outbox 同模式。
-- 危机事件创建时在同一事务内写入 outbox 记录，由 relay 周期扫描投递通知任务，
-- 保证 Redis/入队瞬时故障时通知不会丢失（原实现仅记日志，无补投机制）。
CREATE TABLE IF NOT EXISTS crisis_outbox (
    id           BIGSERIAL   PRIMARY KEY,
    event_id     BIGINT      NOT NULL,
    processed    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_crisis_outbox_pending ON crisis_outbox (processed, created_at) WHERE processed = false;

-- ============================================================================
-- config 域
-- ============================================================================

CREATE TABLE IF NOT EXISTS ai_providers (
    id                 BIGSERIAL    PRIMARY KEY,
    name               VARCHAR(100) NOT NULL UNIQUE,
    provider_type      VARCHAR(20)  NOT NULL,
    api_url            TEXT         NOT NULL,
    api_key_encrypted  BYTEA,
    api_key_masked     VARCHAR(50)  NOT NULL DEFAULT '',
    model_name         VARCHAR(100) NOT NULL,
    dimension          INT,
    parameters         JSONB        NOT NULL DEFAULT '{}'::jsonb,
    is_active          BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT ai_providers_type_chk CHECK (provider_type IN ('llm','embedding','rerank'))
);

-- 对已有库的增量同步：补齐 is_full_url（幂等）。
ALTER TABLE ai_providers ADD COLUMN IF NOT EXISTS is_full_url BOOLEAN NOT NULL DEFAULT FALSE;

-- 下线 rewrite provider：独立查询改写模型已并入统一理解与审查（Assessor）。
-- 先清理存量数据再收紧 CHECK 约束（顺序不可颠倒，否则约束校验失败）。
DELETE FROM ai_providers WHERE provider_type = 'rewrite';
ALTER TABLE ai_providers DROP CONSTRAINT IF EXISTS ai_providers_type_chk;
ALTER TABLE ai_providers ADD CONSTRAINT ai_providers_type_chk CHECK (provider_type IN ('llm','embedding','rerank'));

CREATE INDEX IF NOT EXISTS idx_ai_providers_type_active ON ai_providers (provider_type, is_active);

CREATE TABLE IF NOT EXISTS sensitive_words (
    id          BIGSERIAL   PRIMARY KEY,
    word        VARCHAR(100) NOT NULL,
    category    VARCHAR(30)  NOT NULL,
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (word, category),
    CONSTRAINT sensitive_words_cat_chk CHECK (category IN ('suicide','emergency','injection'))
);

CREATE INDEX IF NOT EXISTS idx_sensitive_words_category ON sensitive_words (category);

CREATE TABLE IF NOT EXISTS safety_rules (
    id          BIGSERIAL   PRIMARY KEY,
    name        VARCHAR(100) NOT NULL UNIQUE,
    pattern     TEXT        NOT NULL,
    action      VARCHAR(30) NOT NULL,
    category    VARCHAR(30) NOT NULL DEFAULT 'other',
    replacement TEXT        NOT NULL DEFAULT '',
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    description TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT safety_rules_cat_chk CHECK (category IN ('diagnosis','prescription','stop_medication','delay_medical','other'))
);

CREATE INDEX IF NOT EXISTS idx_safety_rules_enabled  ON safety_rules (is_active);
CREATE INDEX IF NOT EXISTS idx_safety_rules_category ON safety_rules (category);

CREATE TABLE IF NOT EXISTS rag_configs (
    id                    BIGSERIAL    PRIMARY KEY,
    chunk_size            INT          NOT NULL DEFAULT 500,
    chunk_overlap         INT          NOT NULL DEFAULT 50,
    max_chunks            INT          NOT NULL DEFAULT 10,
    top_k                 INT          NOT NULL DEFAULT 5,
    similarity_threshold  NUMERIC(4,3) NOT NULL DEFAULT 0.500,
    rerank_enabled        BOOLEAN      NOT NULL DEFAULT FALSE,
    rerank_threshold      NUMERIC(4,3) NOT NULL DEFAULT 0.500,
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT rag_configs_singleton CHECK (id = 1),
    CONSTRAINT rag_chunk_size_chk CHECK (chunk_size BETWEEN 200 AND 2000),
    CONSTRAINT rag_chunk_overlap_chk CHECK (chunk_overlap BETWEEN 0 AND 500),
    CONSTRAINT rag_max_chunks_chk CHECK (max_chunks BETWEEN 1 AND 50),
    CONSTRAINT rag_top_k_chk CHECK (top_k BETWEEN 1 AND 50),
    CONSTRAINT rag_similarity_chk CHECK (similarity_threshold BETWEEN 0 AND 1),
    CONSTRAINT rag_rerank_threshold_chk CHECK (rerank_threshold BETWEEN 0 AND 1)
);

-- 对已有库的增量同步：移除已废弃的 diversity_factor（幂等）。
ALTER TABLE rag_configs DROP COLUMN IF EXISTS diversity_factor;
-- 对已有库的增量同步：移除已废弃的 ood_threshold（纯向量单闸后不再需要 OOD 检测，幂等）。
ALTER TABLE rag_configs DROP COLUMN IF EXISTS ood_threshold;

CREATE TABLE IF NOT EXISTS prompt_templates (
    id            BIGSERIAL   PRIMARY KEY,
    type          VARCHAR(30) NOT NULL,
    version       INT         NOT NULL,
    content       TEXT        NOT NULL,
    is_active     BOOLEAN     NOT NULL DEFAULT FALSE,
    description   TEXT        NOT NULL DEFAULT '',
    department_id BIGINT      REFERENCES departments(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT prompt_type_chk CHECK (type IN ('system')),
    UNIQUE (type, version)
);

CREATE INDEX IF NOT EXISTS idx_prompt_templates_type ON prompt_templates (type);
CREATE INDEX IF NOT EXISTS idx_prompt_templates_dept ON prompt_templates (department_id) WHERE department_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_prompt_templates_active_per_type_dept
    ON prompt_templates (type, COALESCE(department_id, 0)) WHERE is_active = TRUE;

CREATE TABLE IF NOT EXISTS safety_messages (
    id          BIGSERIAL   PRIMARY KEY,
    type        VARCHAR(40) NOT NULL UNIQUE,
    content     TEXT        NOT NULL,
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT safety_msg_type_chk CHECK (
        type IN ('rejection','emergency','safety_warning','crisis_response','no_knowledge','system_error')
    )
);

CREATE TABLE IF NOT EXISTS config_audit_logs (
    id            BIGSERIAL    PRIMARY KEY,
    action        VARCHAR(50)  NOT NULL,
    entity_type   VARCHAR(50)  NOT NULL,
    entity_id     BIGINT,
    operator_id   BIGINT       NOT NULL,
    operator_role VARCHAR(20)  NOT NULL,
    changes       JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_config_audit_logs_entity   ON config_audit_logs (entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_config_audit_logs_operator ON config_audit_logs (operator_id, created_at DESC);

CREATE TABLE IF NOT EXISTS vectorize_outbox (
    id           BIGSERIAL    PRIMARY KEY,
    article_id   BIGINT       NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    processed    BOOLEAN      NOT NULL DEFAULT FALSE,
    processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_vectorize_outbox_pending ON vectorize_outbox (processed, created_at) WHERE processed = false;

-- ============================================================================
-- notification 域
-- ============================================================================

CREATE TABLE IF NOT EXISTS notifications (
    id                 BIGSERIAL    PRIMARY KEY,
    recipient_role     VARCHAR(20)  NOT NULL,
    recipient_dept_id  BIGINT,
    type               VARCHAR(30)  NOT NULL,
    title              VARCHAR(200) NOT NULL,
    body               TEXT         NOT NULL DEFAULT '',
    ref_id             VARCHAR(50),
    is_read            BOOLEAN      NOT NULL DEFAULT false,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_notifications_unread ON notifications (recipient_role, is_read, created_at DESC) WHERE NOT is_read;

-- ============================================================================
-- 邀请码（PATIENT 注册强制邀请码）
-- ============================================================================

CREATE TABLE IF NOT EXISTS invite_codes (
    id          BIGSERIAL    PRIMARY KEY,
    code        CHAR(6)      NOT NULL UNIQUE,
    role        VARCHAR(20)  NOT NULL DEFAULT 'PATIENT',
    created_by  BIGINT       NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    used_by     BIGINT       REFERENCES users(id) ON DELETE SET NULL,
    used_at     TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ  NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT invite_codes_role_chk CHECK (role IN ('SUPER_ADMIN','DEPT_ADMIN','DOCTOR','NURSE','PATIENT')),
    CONSTRAINT invite_codes_used_pair_chk CHECK ((used_by IS NULL) = (used_at IS NULL))
);

-- code 已有 UNIQUE 约束索引，满足按 code 精确查找；无需额外的 now()-谓词部分索引
--（now() 为 STABLE，不能用于索引谓词）。仅保留创建时间倒序索引供管理员列表排序。
CREATE INDEX IF NOT EXISTS idx_invite_codes_created ON invite_codes (created_at DESC);