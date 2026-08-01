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
| 错误处理 | `error-handling.md` | errcheck + gosec G104 + `.harness/constraints/arch/internal/harness/arch/arch_test.go` |
| 日志 | `logging.md` | forbidigo（禁 fmt.Print*/log）+ depguard（禁 logrus） |
| 测试 | `testing.md` | go test + depguard（禁 testify）+ 覆盖率门禁 |
| 依赖注入 | `di.md` | `internal/harness/arch/arch_test.go`（依赖方向）+ 编译期接口断言 |

> **原则**：文档写"为什么 + 标准模式"，`.golangci.yml` 与 `internal/harness/arch/arch_test.go` 写"具体拦截什么"。两者互补不重复。
