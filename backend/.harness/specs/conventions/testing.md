---
last_updated: 2026-07-24
status: active
owner: backend-team
---

# 测试规范

## 为什么
测试是约束层的第一道防线（构建系统级）。一致的风格让测试可读、可维护、可在 CI 机械执行。

## 工具与禁用
- **唯一测试库**：标准库 `testing`。**禁止 testify**（depguard `test-only` 规则拦截）。
- **断言**：用 `t.Errorf`/`t.Fatalf` 手写，错误处理用 `errors.As`。
- **mock**：手写 stub 结构体实现接口（见 `tests/api_contract_test.go` 的 `stubArticleRepo`），不用 mock 库。
- **HTTP 测试**：`net/http/httptest`（`httptest.NewRequest` + `httptest.NewRecorder`）。

## 测试组织
| 层级 | 位置 | 测什么 |
|------|------|-------|
| 单元测试 | `<pkg>/<file>_test.go` | service/repo/工具函数的纯逻辑 |
| API 契约测试 | `tests/api_contract_test.go` | 55 端点路由完整性 + 鉴权门禁 + 响应格式（不依赖 DB/Redis） |
| Schema 测试 | `tests/schema/schema_test.go` | 迁移文件与代码模型一致性 |
| e2e | `tests/e2e_api/` | 完整请求链路（依赖 DB/Redis） |

## 命名
- 测试函数：`Test<Subject>_<Scenario>`（`TestAuthGate_ProtectedEndpoints_RequireToken`）。
- 子测试：`t.Run("<method>_<path>", ...)`。
- 测试辅助：小写开头（`signTestToken`、`doRequest`），导出仅给跨包测试用。

## TestMain 模式
跨包共享的初始化（生成密钥、构造 Authenticator、构建路由树）放 `TestMain`：
```go
func TestMain(m *testing.M) {
    // 生成 RSA 密钥、构造 testAuth、构建 testRouter
    os.Exit(m.Run())
}
```

## 表驱动测试
端点清单等用表驱动（`allEndpoints []endpoint`），断言"非 404"/"401"/"403"：
```go
for _, ep := range protectedEndpoints {
    t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
        rec := doRequest(ep.method, ep.path, "")
        if rec.Code != http.StatusUnauthorized {
            t.Errorf("expected 401, got %d", rec.Code)
        }
    })
}
```

## 契约测试的隔离原则
- API 契约测试**不依赖外部服务**（DB/Redis/LLM）：service 传 nil，靠中间件先拦截；公开端点用 stub repo。
- `fakeRateLimiter` 用不可达 Redis 地址快速失败返回 503，不测限流逻辑本身。
- 端点数硬编码断言（`tests/api_contract_test.go` 中 `if len(allEndpoints) != N`，N 是当前端点数）：新增/删除端点必须同步更新 `allEndpoints` 表与该数字——防止 router 与契约表漂移。**具体数字以代码为准**，本 spec 不记录，避免双向维护漂移。

## 诊断测试隔离（debug build tag）
依赖真实 DB / LLM / 外部 API 的诊断性 e2e 测试（`tests/e2e_api/debug_*_test.go`）**必须**在首行加 `//go:build debug`：
```go
//go:build debug

package e2e_api_test
```
**理由**：这类测试需要联网、调用付费 LLM、依赖环境变量，不应进标准 CI（P0）。
**运行**：`go test -tags debug ./tests/e2e_api/` —— 默认 `go test ./...` 不编译这些文件。
**判定**：是否需要 `//go:build debug` 的标准是"是否依赖不可在 CI 沙箱里满足的外部服务"。本地快速回归、纯逻辑、契约测试**不**加。

## 覆盖率门禁
- **floor 而非 ceiling**：当前 floor 设为 service 层 60%（ratchet，随测试基建成熟度上调）。
- 命令：`go test ./internal/... -cover -coverprofile=coverage.out`
- 测试代码豁免部分 linter（`_test.go` 豁免 gocyclo/funlen/errcheck/gosec/lll/unparam，见 `.golangci.yml`）。

## 机械化执行
- `depguard test-only`：测试文件禁用 testify。
- `go test ./...`：CI 门禁 P0。
- API 契约测试：`go test ./tests/ -run 'TestRouteIntegrity|TestAuthGate|TestRoleGate'`。
