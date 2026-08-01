---
last_updated: 2026-07-30
status: active
owner: backend-team
---

# 架构概览

## 技术栈
- **语言**：Go 1.25（module `health-nexus`，go.mod 声明 go 1.25）
- **HTTP 路由**：chi/v5（轻量、原生 `net/http` 兼容）
- **数据库**：PostgreSQL + pgvector（向量检索）+ pgx/v5 手写 SQL（未使用 ORM/sqlc 生成）
- **缓存/队列**：Redis（go-redis/v9）+ asynq（异步任务，wiki 向量化）
- **认证**：JWT HS256（`golang-jwt/jwt/v5`，对称密钥 `HEALTH_NEXUS_JWT_SECRET`），refresh token 轮换 + Redis 黑名单
- **LLM**：`go-openai` 客户端，多 provider 分离（chat/embedding/rerank/rewrite）
- **配置**：viper（`config.yaml` + `HEALTH_NEXUS_*` 环境变量自动绑定）
- **日志**：`log/slog`（标准库，结构化日志）
- **密码学**：argon2id（密码哈希）+ AES-GCM（API Key 字段级加密）
- **迁移**：goose（`migrations/*.sql`，Up/Down 对称）

## 模块划分

`internal/` 下按职责分目录，遵循 Go 的 internal 包可见性约束：

```
cmd/                          可执行入口（server/worker/hashpw/seed）
internal/
├── config/                   配置加载（viper + 结构体映射）
├── di/                       手写依赖注入（NewApp 装配全部域）
├── adapter/                  跨域适配器（domain 之间的桥接，打破循环依赖）
├── middleware/               HTTP 中间件（JWT/CORS/限流/日志/隔离/恢复）
├── platform/                 基础设施（postgres/redis/asynq/llm/crypto/logger）
├── shared/                   跨域共享原语（errors/response/constants/contextkeys 等）
└── domain/                   业务域（5 个，每个域内部分 4 层）
    ├── auth/                 认证：登录/注册/刷新/登出/密码重置
    ├── base/                 基础数据：科室树
    ├── chat/                 对话：SSE 流式 + 危机干预 + RAG
    ├── config/               配置管理：AI provider/敏感词/安全规则/RAG/Prompt
    └── wiki/                 知识库：文章 5 态状态机 + 引用授权 + 向量检索
tests/                        跨域测试（API 契约 + e2e + schema）
migrations/                   SQL 迁移（数据模型唯一真源）
```

### 域内分层（每个 domain/<name>/ 下）
- `entity/`：纯数据结构，不依赖任何框架
- `repository/`：数据访问（pgx 手写 SQL），未找到返回 `(nil, nil)`
- `service/`：业务逻辑 + 事务编排，定义消费者接口（Ports）
- `handler/`：HTTP 协议适配 + 路由注册（`Mount`/`RegisterRoutes`）

## 部署拓扑
- **server**（`cmd/server`）：HTTP API 服务，依赖 PostgreSQL + Redis + LLM API
- **worker**（`cmd/worker`）：asynq 异步 worker，处理 wiki 文章向量化 + 复审逾期扫描，依赖 PostgreSQL + Redis + LLM Embedding API
- **外部依赖**：PostgreSQL（pgvector 扩展）、Redis、多个 LLM provider（硅基流动/智谱/OpenAI 兼容）

## 关键设计决策
- **手写 DI**（替代 google/wire）：`internal/di/app.go` 显式构造，依赖关系清晰可控
- **消费者接口**：service 层定义所需的数据访问接口（如 `UserRepo`），repository 实现该接口，di 层注入——依赖方向反转
- **跨域适配器**（`internal/adapter/`）：域间协作通过 adapter 桥接（如 `BaseDepartmentResolver`），避免域之间直接 import
- **安全 fail-closed**：refresh token 轮换依赖 Redis SETNX，Redis 故障时拒绝（503）而非放行

## E2E 测试修复记录

> 来源：`tests/e2e_api/` 端到端回归。下列为已修复并固化为约束的问题，记录以防回归。chat 域请求流相关修复见 `architecture/data-flow.md` 第 4 节。

### 迁移系统（goose）
- **goose 注解缺失**：`00003`/`00004`/`00006` 早期迁移缺少 `-- +goose Up`/`-- +goose Down` 注解，goose 无法解析方向，已补齐（遵循 Up/Down 对称约定）。
- **版本号冲突**：新增迁移与已发布版本撞号，重命名为 `00026`–`00029`，保证版本号单调递增、全局唯一。
- **幂等性**：`00028` 的 Down 使用 `DROP CONSTRAINT IF EXISTS`，重复执行/回滚不报错。

### 数据模型一致性（auth 域 user_repo）
- repository SQL 与迁移 schema 对齐：移除已废弃的 `avatar_url` 列引用；补齐 `phone`/`date_of_birth`/`gender`/`emergency_contact`/`emergency_phone` 列读写，消除 schema 不匹配导致的扫描错误。

### 业务规则（base 域 department_service）
- **同级科室重名校验**：新增 `SiblingNameExists` 查询 + `assertNameUnique` 断言，创建/重命名科室时校验同一父级下名称唯一，避免重名混淆。

### 前端（H5，仅手机端）
- **安全区适配**：HTML viewport 追加 `viewport-fit=cover`，页面布局采用 `env(safe-area-inset-*)` 处理刘海屏/底部 Home 指示条，避免内容被遮挡（契合本项目「仅适配手机端」原则）。

### 第二轮边缘测试修复（2026-07-31）
- **安全关键词归一化**（`safety_filter.go`）：新增 `normalizeForMatch` 函数，剥离空格/零宽字符/全角空格后再做子串匹配，防止"自 杀"等插入字符绕过。
- **英文危机关键词**（`safety_filter.go`）：`defaultSensitiveWords.Suicide` 补充 kill myself/suicide/suicidal/end my life/want to die/self-harm。
- **文章输入校验收紧**（`article_service.go`）：content/title 改用 `strings.TrimSpace` 判空；新增 `titleMaxRunes=255` 前置长度校验，将 DB VARCHAR(255) 约束冲突从 500 收敛为 422。
- **注册验证状态码统一**（`auth_service.go`）：`validateUsername`/`validatePasswordStrength` 从 BadRequest(400) 改为 Validation(422)。
- **并发锁释放日志**（`chat_send_service.go`）：unlock 失败从静默吞错改为 `slog.WarnContext` 记录。
- **数据修复**：`sensitive_words` 表"自杀"误分类为 emergency → 删除，使 emergency 分类回退默认症状列表。
