# 开发最高原则与工作流

## 项目概述
AI 驱动的医院健康宣教平台，使用 RAG 技术实现 7x24 智能健康问答，为患者提供可溯源的健康指导，减轻医护重复宣教负担。
- **只适配手机端**： 本项目的前端仅使用H5页面进行访问，所有的前端页面适配手机端
- **架构**：Monorepo 前后端分离。后端 `backend/`，前端 `frontend/`（各自技术栈见子项目 CLAUDE.md）
- **后端架构**：DDD 限界上下文（base / auth / wiki / chat / config）+ 三层分离（Handler → Service → Repository）+ 手写 DI + 手写 SQL (pgx)
- **前端架构**：双 SPA（患者端 `chat`、医护端含管理 `staff`），API 基路径 `/api/`，含 `/api/auth/*`、`/api/base/*`、`/api/wiki/*`、`/api/staff/wiki/*`、`/api/chat/*`、`/api/staff/chat/*`、`/api/staff/config/*`、`/healthz`

## 子项目导航
本文件仅保留跨子项目的总体描述与通用原则。各子项目的细节（专属项目概述、知识导航、构建验证命令、专属硬性规则）见各自 CLAUDE.md：

| 子项目 | 说明 | 入口 |
|--------|------|------|
| 后端 | API 服务 + RAG 引擎 + 知识库 | [backend/CLAUDE.md](backend/CLAUDE.md) |
| 前端 | 患者端 + 医护端 SPA | [frontend/CLAUDE.md](frontend/CLAUDE.md) |

## 全局硬性规则
跨子项目通用红线，子项目专属规则见各自 CLAUDE.md。

- **不可逆操作**：禁止 force push main；禁止 `rm -rf`。
- **密钥/凭证**：禁止读取或修改任何密钥与凭证文件（`*.pem`、`*.key`、`.env*`、`config.local.yaml` 等）。具体密钥存放位置与覆盖规则见各子项目。
- **生产配置**：仓库内默认配置仅供开发；生产环境须用环境变量覆盖密钥与 API Key。

## Ponytail, lazy senior dev mode

Lazy = efficient, not careless. Best code is code never written. Understand the problem first (read task, read code, trace flow), then climb the ladder:

1. Need to build at all? (YAGNI)
2. Already in codebase? Reuse.
3. Stdlib does it? Use it.
4. Native platform feature? Use it.
5. Installed dependency solves it? Use it.
6. One line? One line.
7. Else: write the minimum that works.

Bug fix = root cause: grep every caller, fix the shared function once.

- No unrequested abstractions/dependencies/boilerplate. Deletion over addition.
- Shortest working diff wins — but only after understanding the problem.
- Mark intentional shortcuts with `ponytail:` comment (name the ceiling + upgrade path).

Not lazy about: understanding the problem, input validation at trust boundaries, error handling that prevents data loss, security, anything explicitly requested. Non-trivial logic leaves ONE runnable check.

## 工作流
- 新功能：先更新需求文档 → 写核心测试 → 实现 → 测试（类 TDD）

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

When the user types `/graphify`, use the installed graphify skill or instructions before doing anything else.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- Dirty graphify-out/ files are expected after hooks or incremental updates; dirty graph files are not a reason to skip graphify. Only skip graphify if the task is about stale or incorrect graph output, or the user explicitly says not to use it.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

