# CLAUDE.md — health-nexus backend

> 本文件仅描述后端子项目。跨项目通用原则（MCP 工具优先策略、Ponytail、TDD 工作流、全局安全红线）见根目录 [../CLAUDE.md](../CLAUDE.md)。

## 项目概述
医疗健康对话后端（RAG + 危机干预 + 知识库管理）。Go 1.25 + chi + pgx/v5 + PostgreSQL(pgvector) + Redis + asynq + JWT(HS256) + log/slog。

## 知识导航
| 你需要… | 去这里 |
|---------|-------|
| 架构/分层/数据流 | `.harness/specs/architecture/` |
| 编码约定（命名/错误/日志/测试/DI） | `.harness/specs/conventions/<topic>.md` |
| 错误码表 | `.harness/specs/reference/error-codes.md` |
| 70 端点契约 + 鉴权门禁 | `tests/api_contract_test.go` |
| 前端适配 API 路由地图（自动生成，含 handler 限定名） | `docs/api-contract.md` |
| 架构约束规则（AC-ARCH-*） | `internal/harness/arch/arch_test.go` |
| lint 配置 | `.golangci.yml` |
| 数据模型 | `migrations/` |

## 构建与验证

**全量门禁**（P0+P1+P2，含 build/vet/arch/契约/单元测试/lint/漏洞扫描/gofmt/覆盖率）：
```bash
.harness/constraints/ci/gate.sh           # 全跑
.harness/constraints/ci/gate.sh p0        # 仅 P0
```

**快速预检**（秒级子集）：
```bash
go build ./... && go vet ./... && go test ./internal/... -count=1
```

**运行服务**：`air`（见 `.air.toml`）

## 硬性规则
- **密钥**：禁止读取/修改 `*.key`、`config.local.yaml`、`.env*`。JWT 使用 HS256 对称密钥（环境变量 `HEALTH_NEXUS_JWT_SECRET`），生产须用环境变量覆盖。
- **生产配置**：`config.yaml` 的 API Key / `encryption_key` 仅供开发；生产须用 `HEALTH_NEXUS_*` 环境变量覆盖。空 `encryption_key` 启动时 panic。
- **不可逆操作**：禁止修改已提交迁移（force push / rm -rf 等通用红线见根目录 CLAUDE.md）。
- **认证**：受保护端点必经 `JWTAuth`；config 域额外需 `RequireAdmin()`；角色隔离由 `RequireRole` 强制。
- **安全优先**：refresh token 轮换/登出在 Redis 故障时 fail-closed（503）。
- **错误不泄露**：未知错误统一 500 `INTERNAL_ERROR`；登录失败统一 `AUTH_INVALID_CREDENTIALS`。
- **禁用依赖**：`testify`、`logrus`、`pkg/errors`、`golang/protobuf`（depguard 拦截）。
