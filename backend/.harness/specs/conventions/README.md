---
last_updated: 2026-07-23
status: active
owner: backend-team
---

# 编码约定总览

本目录是 health-nexus 的编码约定（规格层"是什么"的文档形态，配合 `.golangci.yml` 的代码配置）。

| 约定 | 文档 | 机械化执行 |
|------|------|-----------|
| 命名 | `naming.md` | revive `var-naming` + goimports |
| 错误处理 | `error-handling.md` | errcheck + gosec G104 + `internal/harness/arch/arch_test.go` |
| 日志 | `logging.md` | forbidigo（禁 fmt.Print*/log）+ depguard（禁 logrus） |
| 测试 | `testing.md` | go test + depguard（禁 testify）+ 覆盖率门禁 |
| 依赖注入 | `di.md` | `internal/harness/arch/arch_test.go`（依赖方向）+ 编译期接口断言 |

> **原则**：文档写"为什么 + 标准模式"，`.golangci.yml` 与 `internal/harness/arch/arch_test.go` 写"具体拦截什么"。两者互补不重复。

## 门禁规则索引

每个阻断规则必须可查到：规则 ID、守护的风险、适用范围、执行阶段、验证入口、判定标准、维护者、例外策略。

- **维护者**：backend-team（见本文件 frontmatter `owner`）。
- **阈值与参数**以 `.harness/constraints/ci/gate.sh` 与 `.golangci.yml` 为准；本表只登记归属与判定，不重复参数，避免多处手工同步。
- **执行阶段**：本地预检 / 合并门禁 / 发布门禁 / 定期检查。发布门禁用 `GATE_STRICT=1`，被跳过的检查按失败处理。
- **结果状态**：PASS / FAIL / ERROR / SKIP / WAIVED，**SKIP 不等于 PASS**（没有运行不得记成通过）。

| 规则 ID | 守护的风险 | 阶段 | 验证入口 | 判定 / 例外策略 |
|---|---|---|---|---|
| BE-BUILD | 制品无法编译 | 合并 | P0-1 `go build ./...` | 失败即阻断 |
| BE-VET | 可疑构造（如 printf 类型不匹配） | 合并 | P0-2 `go vet ./...` | 失败即阻断 |
| AC-ARCH-01..16 | 分层依赖倒置、跨域耦合、未落入已定义层的文件 | 合并 | P0-3 `go test ./internal/harness/arch/` | 任一违规即阻断；存量按规则逐条排除，不做全库豁免 |
| BE-CONTRACT | 路由 / 鉴权 / 角色门禁漂移 | 合并 | P0-4 `TestRouteIntegrity` `TestAuthGate` `TestRoleGate` | 失败即阻断 |
| BE-APIMAP | 路由地图与代码不同步 | 合并 | P0-4b `TestGenerateAPIContract` | 无法生成即阻断 |
| BE-UNIT | 功能回归 | 合并 | P0-5 `go test ./internal/...` | 失败即阻断 |
| BE-RACE | 数据竞争 | 合并 | P0-6 `go test -race ./internal/...` | 失败即阻断；无 cgo/gcc 时本地降级告警、CI/发布阻断 |
| BE-LINT | 死代码、魔法值、错误未处理、安全缺陷、超长函数、禁用依赖 | 合并 | P0-7 `golangci-lint run ./...` | 失败即阻断；例外写在 `.golangci.yml` 的 `exclusions` 并注明理由 |
| BE-DEADCODE | 仅被未用导出间接引用的死函数 | 合并 | P0-7b `deadcode ./...` | 发现即阻断 |
| BE-DB-IDEMPOTENT | 库结构与种子非单一真源、DDL 不可安全重放 | 合并 | P0-8 `internal/di/{schema,seed}.sql` 存在性 + 幂等关键字 | 缺失或非幂等即阻断 |
| BE-VULN | 已知依赖漏洞 | 合并 | P1-1 `govulncheck ./...` | 命中即阻断；无修复版本时登记带期限的例外（写明风险、期限与处置人） |
| BE-FMT | 未格式化代码入库 | 合并 | P1-2 `gofmt -l` / P1-3 `goimports -l` | 有文件即阻断；goimports 缺失由 golangci-lint 的 goimports 覆盖 |
| BE-E2E | 关键用户流程失效 | 发布 | P1-5 Playwright（需前后端已启动） | 失败即阻断；服务未起时本地告警、发布阻断 |
| BE-EVAL | 高风险漏判 / 误拦超标 | 发布 | P1-6 `go test -tags eval ./tests/eval/...` | 未达标即阻断；无 API Key 时本地告警、发布阻断（评测集通过≠产品安全） |
| BE-COV-FLOOR | 未验证区域扩大 | 定期 | P2-1 service 层覆盖率 < 60% | 仅告警（ratchet floor）；不作为合并阻断，也不追求单一覆盖率数字 |
| BE-HYGIENE | TODO 进生产、外网 CDN、敏感文件或大文件入库 | 合并 | P2-2..P2-6 | CDN 与敏感文件阻断；TODO、大文件、过期设计文档告警 |
| BE-DUP | 近似代码片段（克隆） | 审查 | P0-7 `dupl`（作用域经 exclusions 收窄到 service/platform/shared） | 当前阻断；克隆检测无法可靠判定"不同写法的相同业务含义"，本应只作审查信号——收敛方向见下 |

**已知需要收敛的一点**：`dupl`（阻断）与前端 `jscpd`（仅告警）是两条语义相同的克隆检测，却有不同的阻断策略。克隆检测无法可靠识别"不同写法的相同业务含义"，严格按此口径 `dupl` 应降为告警；但它的作用域已收窄到"两份实现会各自漂移"的逻辑层，是在能力入口约束落地前的临时防线。**该降级需显式决策并留记录，不得静默放宽**。

**需要人工判断、不进自动门禁的**：错误码命名与登记一致性（P1-4 不设检查）、疑似语义重复的归属判定、是否应抽象、模块职责是否合理。这些由 review checklist 覆盖，AI 审查只提供候选与证据。
