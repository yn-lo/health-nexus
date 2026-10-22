# 开发最高原则与工作流

## 项目概述
AI 驱动的医院健康宣教平台，使用 RAG 技术实现 7x24 智能健康问答，为患者提供可溯源的健康指导，减轻医护重复宣教负担。
- **只适配手机端**： 本项目的前端仅使用H5页面进行访问，所有的前端页面适配手机端
- **架构**：Monorepo 前后端分离。后端 `backend/`，前端 `frontend/`（各自技术栈见子项目 AGENTS.md）
- **后端架构**：DDD 限界上下文（base / auth / wiki / chat / config）+ 三层分离（Handler → Service → Repository）+ 手写 DI + 手写 SQL (pgx)
- **前端架构**：双 MPA（患者端 `chat.html`、医护端含管理 `staff.html`，Vite MPA 多入口），API 基路径 `/api/`，含 `/api/auth/*`、`/api/base/*`、`/api/public/*`、`/api/wiki/*`、`/api/staff/wiki/*`、`/api/chat/*`、`/api/staff/chat/*`、`/api/staff/config/*`、`/api/staff/auth/*`、`/api/staff/base/*`、`/healthz`

## 子项目导航
本文件仅保留跨子项目的总体描述与通用原则。各子项目的细节（专属项目概述、知识导航、构建验证命令、专属硬性规则）见各自 AGENTS.md：

| 子项目 | 说明 | 入口 |
|--------|------|------|
| 后端 | API 服务 + RAG 引擎 + 知识库 | [backend/AGENTS.md](backend/AGENTS.md) |
| 前端 | 患者端 + 医护端 SPA | [frontend/AGENTS.md](frontend/AGENTS.md) |

门禁入口与结果状态语义见各子项目的门禁规则索引：[backend/.harness/specs/conventions/README.md](backend/.harness/specs/conventions/README.md)、[frontend/.harness/constraints/README.md](frontend/.harness/constraints/README.md)。要点：单一门禁入口（Bash/PowerShell 包装只负责启动与透传退出码）；结果状态 PASS / FAIL / ERROR / SKIP / WAIVED，**SKIP 不等于 PASS**；发布门禁用 `GATE_STRICT=1`，被跳过的检查按失败处理。

## 编码准则（冲突时按序裁决）

1. 不要假设，不隐藏困惑：不确定就问，主动暴露权衡。
2. 明确成功标准；验证通过前持续迭代，不宣称完成。
3. 绝不吞错误：禁止 rescue nil / 宽泛 catch / 静默默认值；优先快速失败，最短 diff 不构成吞异常的理由。
4. 只在系统边界校验（用户输入 / 外部 API / 网络），信任内部代码和框架保证；禁止为「不可能发生」的场景添加错误处理、回退或判空。
5. 功能迭代不考虑向后兼容，优先追求最新需求的最优实现；发现架构问题就地修复，禁止用兼容层、适配器、防御性 try-catch 绕过。
6. 最小实现（YAGNI）：一行能解决不写多行；禁止推测性功能、预留扩展点、占位函数、"以防万一"的参数与抽象。
7. 最短 diff 获胜，删减优于新增；但删减仅限本次触碰的路径，不顺手重构。
8. 一次性操作禁止抽函数/工具类/抽象；接口仅在 2+ 实现或需 mock 时添加。
9. 未使用的函数、注释掉的代码、"以后可能用到"的逻辑一律删除。Git 保存历史，删除是安全的。
10. 分层为上限非清单：Handler→Service→Repository→Model，简单功能允许跳层；领域逻辑归 Service，Handler 只做校验与参数解析；不加聚合根/值对象。
11. SOLID / DDD 教条与前述冲突时一律让位。

## 变更边界与收尾

- **契约例外**：对外 API、持久化数据格式、被外部依赖的契约，变更前先列出影响面，征得我确认。
- **收尾报告**：说明本次删除/修改了什么，以及如何证明未破坏（调用点检查、测试结果）。




