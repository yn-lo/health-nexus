---
last_updated: 2026-07-24
status: active
owner: backend-team
---

# 依赖注入规范

## 为什么
显式手写 DI（替代 google/wire）让依赖关系在编译期可见、可追溯，避免反射魔法的隐式绑定。结合消费者接口，实现依赖反转：service 定义"我需要什么"，repository 实现，di 装配。

## 装配位置
**唯一装配点**：`internal/di/app.go` 的 `NewApp(ctx, cfg)`。所有跨层依赖在此显式构造并注入。禁止在其他地方 new 依赖。

## 装配顺序（NewApp）
1. `NewInfrastructure` → postgres pool / redis / asynq / txmgr / locker / rateLimiter / auth（失败时 `infra.Close()` 防泄漏）
2. base 域 → auth 域 → config 域 → wiki 域 → chat 域（按依赖顺序，后者依赖前者）
3. `success = true` 接管生命周期；`defer` 失败时关闭已创建资源

## 按域装配函数（NewApp 拆分）
`NewApp` 本身保持精简（~25 行），只负责调用按域拆分的装配函数并接管生命周期。每个域一个 `buildXxxDomain(infra, cfg) → (handler, service, ...)`：
```go
// di/app.go
func NewApp(ctx context.Context, cfg *config.Config) (*App, error) {
    infra, err := NewInfrastructure(ctx, cfg)
    if err != nil { return nil, err }
    success := false
    defer func() { if !success { infra.Close() } }()

    authHandler, authSvc, err := buildAuthDomain(infra, cfg)
    if err != nil { return nil, err }
    configHandler, _, aesKey, err := buildConfigDomain(infra, cfg)
    // ...
    success = true
    return &App{...}, nil
}

func buildConfigDomain(infra *Infrastructure, cfg *config.Config) (
    *confighandler.ConfigHandler, *configservice.ConfigService, []byte, error,
) { ... }
```
**约束**：
- 装配函数仅返回该域对外暴露的 handler/service（注入下游/跨域时由 NewApp 显式传参）。
- 域内 repo ↔ service ↔ handler 的连线在装配函数内部完成。
- NewApp 不直接 new 域内 repo，避免散落。

## 消费者接口模式
service 定义所需能力的接口（Ports），而非依赖具体实现：
```go
// service/auth_service.go
type UserRepo interface {
    GetByUsername(ctx context.Context, username string) (*entity.User, error)
    GetByID(ctx context.Context, id int64) (*entity.User, error)
    // ...
}
type RedisClient interface {
    Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
    // ...
}
```
repository 实现，di 注入：
```go
// di/app.go
userRepo := authrepo.NewUserRepo(infra.Pool)
authSvc := authservice.NewAuthService(userRepo, tokenIssuer, infra.Auth, infra.Redis, cfg)
```

> **关键**：接口定义在消费者包（service），不在实现包（repository）。这样 service 不依赖 repository 包，依赖方向反转。

## 跨域协作：adapter 模式
域 A 需要域 B 的能力时，**不直接 import 域 B**，而是在 `internal/adapter/` 定义适配器：
```go
// adapter/wiki_adapter.go
type BaseDepartmentResolver struct { deptRepo *baserepo.DepartmentRepo }
func (r *BaseDepartmentResolver) Resolve(ctx context.Context, deptID int64) (string, error) { ... }
```
adapter 实现 chat 域 service 定义的接口，di 注入。adapter 是**唯一允许 import 多个 domain 的地方**（破除循环依赖）。

## 编译期接口断言
当 platform 实现需满足 domain 定义的接口时，断言**只能在 di 层完成**（AC-ARCH-09 禁止 platform 反向 import domain）：
```go
// di/app.go
var _ rag.LLMSafetyChecker = (*llm.LLMSafetyChecker)(nil)
```

## 资源生命周期
- `App.Close()` / `Infrastructure.Close()`：关闭 pool/redis/asynq，由 main 在 graceful shutdown 后调用。
- `defer infra.Close()` 在 `NewApp` 失败路径生效，防止连接泄漏。
- LLM 客户端**不进 Infrastructure**——worker 共享装配流程但不需要 LLM，避免强迫 worker 配 `LLM_API_KEY`。

## 机械化执行
- `internal/harness/arch/arch_test.go`：验证依赖方向（platform 不 import domain、handler 不 import repository/platform、service 不 import net/http 等）。
- 编译期断言：`var _ Interface = (*Impl)(nil)` 在 di 层。
- 代码审查：新增域是否在 NewApp 装配、是否通过 adapter 跨域。
