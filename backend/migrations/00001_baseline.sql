-- +goose Up
-- +goose StatementBegin

-- ============================================================================
-- 00001_baseline: 合并 00001~00031 的最终 schema
-- 适用于全新生产环境初始化；已有环境请勿执行。
-- 合并来源：
--   00001-00025  原 baseline
--   00026        messages.result_code 增加 PARTIAL
--   00027        crisis_events.conversation_id 外键 CASCADE → RESTRICT
--   00028        rag_configs.ood_threshold（已包含在原 baseline）
--   00029        users 删除 avatar_url，新增 phone/date_of_birth/gender/emergency_contact/emergency_phone
--   00030        messages 新增 feedback 列
--   00031        新增 notifications 表
-- ============================================================================

-- ── 扩展 ────────────────────────────────────────────────────────────────────
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================================
-- base 域
-- ============================================================================

CREATE TABLE departments (
    id           BIGSERIAL    PRIMARY KEY,
    name         VARCHAR(100) NOT NULL,
    parent_id    BIGINT       REFERENCES departments(id) ON DELETE RESTRICT,
    is_public    BOOLEAN      NOT NULL DEFAULT FALSE,
    is_active    BOOLEAN      NOT NULL DEFAULT TRUE,
    description  TEXT         NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE user_departments (
    id            BIGSERIAL   PRIMARY KEY,
    user_id       BIGINT      NOT NULL,
    department_id BIGINT      NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    is_primary    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, department_id)
);

CREATE UNIQUE INDEX uq_user_departments_one_primary
    ON user_departments(user_id) WHERE is_primary = TRUE;
CREATE INDEX idx_departments_parent_id   ON departments (parent_id);
CREATE INDEX idx_user_departments_user_id ON user_departments (user_id);
CREATE INDEX idx_user_departments_dept_id ON user_departments (department_id);

-- ============================================================================
-- auth 域
-- ============================================================================

CREATE TABLE users (
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

CREATE INDEX idx_users_is_deleted ON users (is_deleted) WHERE is_deleted = FALSE;

-- ============================================================================
-- wiki 域
-- ============================================================================

CREATE TABLE articles (
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

-- bigram 中文分词函数（二元字符组，提升 CJK 全文检索召回率）
CREATE OR REPLACE FUNCTION bigram_array(input text)
RETURNS text[] LANGUAGE sql IMMUTABLE STRICT AS $$
  WITH chars AS (
    SELECT array_agg(substring(input, i, 1)) AS arr
    FROM generate_series(1, char_length(input)) AS i
  ),
  unigrams AS (SELECT arr FROM chars),
  bigrams AS (
    SELECT array_agg(arr[i] || arr[i+1]) AS arr
    FROM chars, generate_series(1, array_length(chars.arr, 1) - 1) AS i
  )
  SELECT unigrams.arr || COALESCE(bigrams.arr, '{}'::text[])
  FROM unigrams, bigrams;
$$;

CREATE OR REPLACE FUNCTION bigram_tsvector(input text)
RETURNS tsvector LANGUAGE sql IMMUTABLE STRICT AS $$
  SELECT to_tsvector('simple', array_to_string(bigram_array(input), ' '));
$$;

CREATE OR REPLACE FUNCTION bigram_tsquery(input text)
RETURNS tsquery LANGUAGE sql IMMUTABLE STRICT AS $$
  SELECT to_tsquery('simple', array_to_string(bigram_array(input), '|'));
$$;

CREATE TABLE article_chunks (
    id            BIGSERIAL   PRIMARY KEY,
    article_id    BIGINT      NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    chunk_index   INT         NOT NULL,
    content       TEXT        NOT NULL,
    content_hash  CHAR(64)    NOT NULL DEFAULT '',
    embedding     vector(1024),
    tsv           tsvector,
    is_active     BOOLEAN     NOT NULL DEFAULT TRUE,
    version       INT         NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION article_chunks_tsv_update() RETURNS trigger AS $$
BEGIN
    NEW.tsv := bigram_tsvector(NEW.content);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_article_chunks_tsv
    BEFORE INSERT OR UPDATE OF content ON article_chunks
    FOR EACH ROW EXECUTE FUNCTION article_chunks_tsv_update();

CREATE INDEX idx_article_chunks_embedding
    ON article_chunks USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
CREATE INDEX idx_article_chunks_tsv     ON article_chunks USING gin (tsv);
CREATE INDEX idx_article_chunks_article ON article_chunks (article_id);
CREATE INDEX idx_article_chunks_active  ON article_chunks (article_id, is_active);

CREATE TABLE article_references (
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

CREATE UNIQUE INDEX uq_article_refs_pending
    ON article_references (article_id, target_dept_id) WHERE status = 'pending';
CREATE INDEX idx_article_refs_source ON article_references (source_dept_id);
CREATE INDEX idx_article_refs_target ON article_references (target_dept_id);
CREATE INDEX idx_article_refs_status ON article_references (status);

CREATE TABLE article_audit_logs (
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

CREATE INDEX idx_articles_status_dept    ON articles (status, department_id) WHERE is_deleted = FALSE;
CREATE INDEX idx_articles_author         ON articles (author_id);
CREATE INDEX idx_articles_published_at   ON articles (published_at);
CREATE INDEX idx_articles_review_overdue ON articles (review_overdue_at) WHERE review_overdue = FALSE;
CREATE INDEX idx_article_audit_logs_article ON article_audit_logs (article_id);
CREATE UNIQUE INDEX uq_articles_featured_rank
    ON articles (department_id, featured_rank) WHERE featured_rank > 0;
CREATE INDEX idx_articles_featured
    ON articles (department_id, view_count DESC, published_at DESC)
    WHERE status = 'published' AND is_deleted = false;

-- ============================================================================
-- chat 域
-- ============================================================================

CREATE TABLE conversations (
    id              UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    patient_id      BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    locked_dept_id  BIGINT       REFERENCES departments(id) ON DELETE SET NULL,
    title           VARCHAR(255) NOT NULL DEFAULT '',
    is_archived     BOOLEAN      NOT NULL DEFAULT FALSE,
    last_message_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_conversations_patient_archived
    ON conversations (patient_id, is_archived);
CREATE INDEX idx_conversations_patient_archived_lastmsg
    ON conversations (patient_id, is_archived, last_message_at DESC);
CREATE INDEX idx_conversations_locked_dept ON conversations (locked_dept_id);

CREATE TABLE messages (
    id                UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    conversation_id   UUID         NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role              VARCHAR(20)  NOT NULL,
    content           TEXT         NOT NULL,
    result_code       VARCHAR(20)  NOT NULL DEFAULT '',
    referenced_chunks JSONB        NOT NULL DEFAULT '[]'::jsonb,
    feedback          VARCHAR(10)  DEFAULT NULL,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT messages_role_chk CHECK (role IN ('user','assistant')),
    CONSTRAINT messages_result_chk CHECK (result_code IN ('','ANSWERED','PARTIAL','REJECTED','INTERCEPTED','CRISIS','RATE_LIMITED')),
    CONSTRAINT messages_feedback_chk CHECK (feedback IS NULL OR feedback IN ('up', 'down'))
);

CREATE INDEX idx_messages_conv_created ON messages (conversation_id, created_at);

CREATE TABLE crisis_events (
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

CREATE INDEX idx_crisis_events_patient       ON crisis_events (patient_id);
CREATE INDEX idx_crisis_events_handled       ON crisis_events (is_handled);
CREATE INDEX idx_crisis_events_level         ON crisis_events (level);
CREATE INDEX idx_crisis_events_created       ON crisis_events (created_at DESC);
CREATE INDEX idx_crisis_events_handled_level ON crisis_events (is_handled, level);

-- ============================================================================
-- config 域
-- ============================================================================

CREATE TABLE ai_providers (
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
    CONSTRAINT ai_providers_type_chk CHECK (provider_type IN ('llm','embedding','rerank','rewrite'))
);

CREATE INDEX idx_ai_providers_type_active ON ai_providers (provider_type, is_active);

CREATE TABLE sensitive_words (
    id          BIGSERIAL   PRIMARY KEY,
    word        VARCHAR(100) NOT NULL,
    category    VARCHAR(30)  NOT NULL,
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (word, category),
    CONSTRAINT sensitive_words_cat_chk CHECK (category IN ('suicide','emergency','injection'))
);

CREATE INDEX idx_sensitive_words_category ON sensitive_words (category);

CREATE TABLE safety_rules (
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

CREATE INDEX idx_safety_rules_enabled  ON safety_rules (is_active);
CREATE INDEX idx_safety_rules_category ON safety_rules (category);

CREATE TABLE rag_configs (
    id                    BIGSERIAL    PRIMARY KEY,
    chunk_size            INT          NOT NULL DEFAULT 500,
    chunk_overlap         INT          NOT NULL DEFAULT 50,
    max_chunks            INT          NOT NULL DEFAULT 10,
    top_k                 INT          NOT NULL DEFAULT 5,
    similarity_threshold  NUMERIC(4,3) NOT NULL DEFAULT 0.750,
    rerank_enabled        BOOLEAN      NOT NULL DEFAULT FALSE,
    rerank_threshold      NUMERIC(4,3) NOT NULL DEFAULT 0.500,
    diversity_factor      NUMERIC(4,3) NOT NULL DEFAULT 0.000,
    ood_threshold         NUMERIC(4,3) NOT NULL DEFAULT 0.300,
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT rag_configs_singleton CHECK (id = 1),
    CONSTRAINT rag_chunk_size_chk CHECK (chunk_size BETWEEN 200 AND 2000),
    CONSTRAINT rag_chunk_overlap_chk CHECK (chunk_overlap BETWEEN 0 AND 500),
    CONSTRAINT rag_max_chunks_chk CHECK (max_chunks BETWEEN 1 AND 50),
    CONSTRAINT rag_top_k_chk CHECK (top_k BETWEEN 1 AND 50),
    CONSTRAINT rag_similarity_chk CHECK (similarity_threshold BETWEEN 0 AND 1),
    CONSTRAINT rag_rerank_threshold_chk CHECK (rerank_threshold BETWEEN 0 AND 1),
    CONSTRAINT rag_diversity_factor_chk CHECK (diversity_factor BETWEEN 0 AND 1),
    CONSTRAINT rag_ood_chk CHECK (ood_threshold BETWEEN 0 AND 0.5)
);

CREATE TABLE prompt_templates (
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

CREATE INDEX idx_prompt_templates_type ON prompt_templates (type);
CREATE INDEX idx_prompt_templates_dept ON prompt_templates (department_id) WHERE department_id IS NOT NULL;
CREATE UNIQUE INDEX uq_prompt_templates_active_per_type_dept
    ON prompt_templates (type, COALESCE(department_id, 0)) WHERE is_active = TRUE;

CREATE TABLE safety_messages (
    id          BIGSERIAL   PRIMARY KEY,
    type        VARCHAR(40) NOT NULL UNIQUE,
    content     TEXT        NOT NULL,
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT safety_msg_type_chk CHECK (
        type IN ('rejection','emergency','safety_warning','crisis_response','no_knowledge','system_error')
    )
);

CREATE TABLE config_audit_logs (
    id            BIGSERIAL    PRIMARY KEY,
    action        VARCHAR(50)  NOT NULL,
    entity_type   VARCHAR(50)  NOT NULL,
    entity_id     BIGINT,
    operator_id   BIGINT       NOT NULL,
    operator_role VARCHAR(20)  NOT NULL,
    changes       JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_config_audit_logs_entity   ON config_audit_logs (entity_type, entity_id);
CREATE INDEX idx_config_audit_logs_operator ON config_audit_logs (operator_id, created_at DESC);

CREATE TABLE vectorize_outbox (
    id           BIGSERIAL    PRIMARY KEY,
    article_id   BIGINT       NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    processed    BOOLEAN      NOT NULL DEFAULT FALSE,
    processed_at TIMESTAMPTZ
);

CREATE INDEX idx_vectorize_outbox_pending ON vectorize_outbox (processed, created_at) WHERE processed = false;

-- ============================================================================
-- notification 域
-- ============================================================================

CREATE TABLE notifications (
    id               BIGSERIAL    PRIMARY KEY,
    recipient_role   VARCHAR(20)  NOT NULL,
    recipient_dept_id BIGINT,
    type             VARCHAR(30)  NOT NULL,
    title            VARCHAR(200) NOT NULL,
    body             TEXT         NOT NULL DEFAULT '',
    ref_id           VARCHAR(50),
    is_read          BOOLEAN      NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_notifications_unread ON notifications (recipient_role, is_read, created_at DESC) WHERE NOT is_read;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS notifications CASCADE;
DROP TABLE IF EXISTS vectorize_outbox CASCADE;
DROP TABLE IF EXISTS config_audit_logs CASCADE;
DROP TABLE IF EXISTS safety_messages CASCADE;
DROP TABLE IF EXISTS prompt_templates CASCADE;
DROP TABLE IF EXISTS rag_configs CASCADE;
DROP TABLE IF EXISTS safety_rules CASCADE;
DROP TABLE IF EXISTS sensitive_words CASCADE;
DROP TABLE IF EXISTS ai_providers CASCADE;
DROP TABLE IF EXISTS crisis_events CASCADE;
DROP TABLE IF EXISTS messages CASCADE;
DROP TABLE IF EXISTS conversations CASCADE;
DROP TABLE IF EXISTS article_audit_logs CASCADE;
DROP TABLE IF EXISTS article_references CASCADE;
DROP TRIGGER IF EXISTS trg_article_chunks_tsv ON article_chunks;
DROP TABLE IF EXISTS article_chunks CASCADE;
DROP TABLE IF EXISTS articles CASCADE;
DROP TABLE IF EXISTS users CASCADE;
DROP TABLE IF EXISTS user_departments CASCADE;
DROP TABLE IF EXISTS departments CASCADE;
DROP FUNCTION IF EXISTS article_chunks_tsv_update();
DROP FUNCTION IF EXISTS bigram_tsquery(text);
DROP FUNCTION IF EXISTS bigram_tsvector(text);
DROP FUNCTION IF EXISTS bigram_array(text);

-- +goose StatementEnd
