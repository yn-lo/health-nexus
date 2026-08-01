# Health Nexus

AI 驱动的医院健康宣教平台，基于 RAG（检索增强生成）技术为患者提供 7×24 智能健康问答，为医护减轻重复宣教负担。

> **声明**：本系统为健康宣教工具，不提供在线诊疗、不开处方、不建议停药。

## 技术栈

| 层    | 技术                                                                                             |
| ---- | ---------------------------------------------------------------------------------------------- |
| 后端   | Go 1.25 · Chi v5 · PostgreSQL 16 (pgvector) · Redis 7 · asynq · JWT HS256 · argon2id · AES-GCM |
| 前端   | Vue 3 · TypeScript (strict) · Vite 6 · Tailwind CSS v4 · Pinia · Vant 4 · ECharts              |
| 基础设施 | Docker Compose · goose 迁移 · golangci-lint · Vitest · Playwright                                |

## 架构概览

```
frontend/                  双 SPA（患者端 chat + 医护端 staff）
  src/chat/                患者端：AI 问答、知识浏览、个人中心
  src/staff/               医护端：文章管理、审核、危机事件、系统配置
  src/shared/              API 封装、通用组件、类型、工具

backend/                   DDD 限界上下文 + 三层分离
  cmd/server/              HTTP Server 入口
  cmd/worker/              asynq Worker（异步向量化、复审扫描）
  internal/domain/         5 个限界上下文
    base/                  科室列表
    auth/                  登录、JWT 双 Token、注册
    wiki/                  文章全生命周期、审核、切片向量化
    chat/                  RAG 问答、会话管理、危机事件
    config/                AI Provider、RAG 参数、安全规则、Prompt 模板
  internal/platform/       postgres · redis · asynq · llm · crypto · logger
  internal/shared/         常量 · 错误 · 响应 · 分页 · 掩码
  internal/middleware/     JWT · CORS · 限流 · 数据隔离 · 角色校验
  internal/di/             手写 DI (InitializeApp)
  migrations/              goose SQL 迁移
```

## 核心功能

- **AI 问答（RAG）**：SSE 流式回答、5 轮历史上下文、查询改写、混合检索（向量 + BM25 + Rerank）、可溯源引用
- **安全防护**：双层审查（规则层关键词 + LLM 深度审查）、危机关键词零延迟拦截、Prompt 注入拦截、输出侧越权检测
- **危机事件闭环**：自伤/自杀关键词触发记录 → 医护处理 → 闭环
- **知识库管理**：文章全生命周期（draft → pending → published → archived → deleted）、审核流程、自动切片向量化、180 天复审机制、跨科室引用授权
- **系统配置**：AI Provider 统一管理、API Key 加密存储、敏感词/安全规则/RAG 参数/Prompt 模板管理

## 角色体系

| 角色             | 端   | 权限         |
| -------------- | --- | ---------- |
| SUPER\_ADMIN   | 医护端 | 全局系统配置     |
| DEPT\_ADMIN    | 医护端 | 本科室文章与配置   |
| DOCTOR / NURSE | 医护端 | 文章发布与审核    |
| PATIENT        | 患者端 | AI 问答、知识浏览 |

## 快速开始

### 1. Docker 部署（全栈，推荐）

```bash
docker compose up -d
```

一键构建前后端合并镜像（Dockerfile 在项目根目录），启动 PostgreSQL + Redis + server + worker；前端静态文件由后端容器统一提供。数据持久化到 Docker volume。

> 以下步骤（2–5）用于本地开发（不使用 Docker）。

### 2. 配置环境变量

```bash
cp .env.example .env
# 编辑 .env，填入 LLM API Key、JWT secret（HEALTH_NEXUS_JWT_SECRET）等
```

### 3. 数据库迁移

```bash
goose -dir backend/migrations postgres "$DATABASE_URL" up
```

### 4. 启动后端

```bash
cd backend
go run ./cmd/server
# 另开终端启动 Worker
go run ./cmd/worker
```

### 5. 启动前端

```bash
cd frontend
npm install
npm run dev
```

- 患者端：`http://localhost:5173/chat/`
- 医护端：`http://localhost:5173/staff/`
- API 代理自动转发 `/api` → `http://localhost:8000`

## 验证命令

### 后端（在 `backend/` 目录执行）

```bash
go build ./...                          # 编译
go vet ./...                            # 静态检查
golangci-lint run ./...                 # Lint
go test -race -count=1 ./internal/...   # 单元测试
```

架构约束测试（在 `harness/go/` 目录执行）：

```bash
go test ./constraints/arch/...
```

### 前端（在 `frontend/` 目录执行）

```bash
npx eslint src/                  # Lint
npx vue-tsc --noEmit             # 类型检查
npx vitest run                   # 单元测试
npx vite build                   # 构建
```

### 完整门禁（CI 等价）

```bash
make verify
```

## API 概览

| 路由前缀                 | 端点数     | 鉴权                             |
| -------------------- | ------- | ------------------------------ |
| `/api/auth/`         | 5       | 部分白名单                          |
| `/api/base/`         | 1       | JWT + 医护角色                     |
| `/api/wiki/articles` | 2       | 匿名可读                           |
| `/api/staff/wiki/`   | 14      | JWT + 医护角色                     |
| `/api/chat/`         | 7+1 SSE | JWT + 患者角色                     |
| `/api/staff/chat/`   | 4       | JWT + 医护角色                     |
| `/api/staff/config/` | 18      | JWT + SUPER\_ADMIN/DEPT\_ADMIN |
| `/healthz`           | 1       | 匿名                             |

完整 API 契约见 [api-contracts.md](harness/go/specs/api-contracts.md)。

## 项目文档

| 文档       | 路径                                                                                         |
| -------- | ------------------------------------------------------------------------------------------ |
| 架构概览     | [harness/go/specs/architecture/overview.md](harness/go/specs/architecture/overview.md)     |
| 限界上下文边界  | [harness/go/specs/architecture/boundaries.md](harness/go/specs/architecture/boundaries.md) |
| 数据流      | [harness/go/specs/architecture/data-flow.md](harness/go/specs/architecture/data-flow.md)   |
| 需求规格     | [harness/go/specs/requirements.md](harness/go/specs/requirements.md)                       |
| API 契约   | [harness/go/specs/api-contracts.md](harness/go/specs/api-contracts.md)                     |
| 编码约定     | [harness/go/specs/conventions.md](harness/go/specs/conventions.md)                         |
| 错误码参考    | [harness/go/specs/reference/error-codes.md](harness/go/specs/reference/error-codes.md)     |
| API 测试规范 | [harness/go/specs/api-testing.md](harness/go/specs/api-testing.md)                         |

## License

Private — All rights reserved.
