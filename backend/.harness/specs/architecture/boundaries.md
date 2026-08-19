---
last_updated: 2026-08-19
status: active
owner: backend-team
---

# 分层架构

## 设计意图

health-nexus 采用**按域分层 + 依赖反转**架构。核心目的：

1. **Service 是唯一的事务边界和业务编排点**——业务规则只此一处，可被多个 handler/worker 复用
2. **Handler 只做协议适配**——解析请求、调用 service、组装响应，不承载业务语义
3. **Repository 只做数据映射**——SQL 与实体转换，不承载业务逻辑，未找到返回 `(nil, nil)` 由 service 决策
4. **Entity 是纯数据**——不依赖任何框架，可被任意层引用
5. **Platform 是基础设施**——技术细节封装，不感知业务
6. **Domain 不直接依赖 Platform**——通过 service 定义的消费者接口（Ports）反转依赖，由 di 层注入实现

这样分层的收益：业务逻辑可独立测试（注入 mock 接口）、技术栈可替换（换 ORM 不影响 service）、域间解耦（通过 adapter 桥接而非直接 import）。

## 依赖方向

```
cmd ──► di ──► adapter ──► domain.{auth,base,chat,config,wiki}
                  │              │
                  │              ├─► handler ──► service ──► repository ──► entity
                  │              │      │            │            │
                  │              │      │            └─► platform（经接口注入）
                  │              │      └─► shared / middleware
                  │              └─► shared
                  └─► platform / shared
```

**所有箭头单向，禁止反向依赖。** 具体规则由约束层代码强制执行，参见 `internal/harness/arch/arch_test.go`。

## 各层职责

### entity（领域实体层）
纯数据结构，定义业务对象。不依赖任何框架（无 `net/http`、无 `pgx`、无第三方库）。可被同域的 repository/service/handler 引用。

### repository（数据访问层）
pgx 手写 SQL，实现 service 定义的消费者接口。职责限于：执行 SQL、扫描到 entity、处理 `pgx.ErrNoRows`（返回 `nil, nil`）。不承载业务语义，不返回 `*AppError`（那是 service 的事）。

### service（业务层）
**唯一的事务边界**（经 `platform/postgres.TxManager`）。定义消费者接口（Ports，如 `UserRepo`、`RedisClient`），由 di 注入实现。返回 `*AppError` 表达业务错误。记录关键业务日志（PII 保护：不记 username 明文，仅 user_id/role）。

### handler（协议适配层）
HTTP 协议边界：解析 JSON 请求体（严格模式 `DisallowUnknownFields`）、调用 service、用 `shared/response` 组装 JSON 响应。路由注册（`Mount`/`RegisterRoutes`）。不写业务规则，不直接调 repository，不直接 import platform。

### adapter（跨域适配层）
当域 A 需要域 B 的能力时，由 adapter 桥接（如 chat 域需要 base 域的科室解析 → `adapter.NewBaseDepartmentResolver`）。adapter 可 import 多个 domain，是循环依赖的破除点。adapter 不含业务逻辑，仅做接口适配。

### platform（基础设施层）
技术细节封装：postgres 连接池/事务、redis 客户端/锁、asynq 客户端、llm 客户端、crypto、logger。**禁止反向 import domain**（AC-ARCH-09）。平台若需被 domain 使用，由 service 定义接口、di 注入实现。

### shared（共享原语层）
跨域通用原语：`errors`（AppError）、`response`（JSON 响应）、`constants`、`contextkeys`、`pagination`、`mask`、`contenthash`。**叶子层**，不依赖 domain/platform/middleware（除标准库与极少数基础依赖）。

### middleware（中间件层）
HTTP 中间件：JWT 认证、CORS、限流、请求日志、数据隔离、恢复、请求 ID。可依赖 shared、config。平台能力（如 JWT 公钥、限流存储、token 黑名单）须定义为消费者接口由 di 注入，**禁止直接 import platform 具体实现**（AC-ARCH-15）。handler 通过 chi `r.Use()` 装配。

### di（依赖注入层）
`NewApp` 按序构造基础设施 + 5 个域的 handler/service/repository，注入接口实现。是唯一允许 import 所有层的地方。编译期接口断言（如 `var _ rag.LLMSafetyChecker = (*llm.LLMSafetyChecker)(nil)`）只能在此完成（AC-ARCH-09 禁止 platform 反向 import domain，故断言在 di 层）。

## 具体规则

> 由约束层代码强制执行，参见 `internal/harness/arch/arch_test.go`。每条规则对应一个 `AC-ARCH-*` 编号，与 `.golangci.yml` 的 lint 规则互补：
> - **结构性依赖方向**（谁能 import 谁）→ `arch_test.go`（AST/import 路径检查）
> - **代码风格**（行数/函数长度/错误处理/死代码）→ `.golangci.yml`（funlen/lll/errcheck/staticcheck）
> - **安全**（gosec G304/G306 等）→ `.golangci.yml`（AC-SEC）
> - **无约束层兜底**（AC-ARCH-16）→ 任何源文件必须落入已定义层，`unknown` / `domain-root` / `domain-other` 直接报错，防止文件塞进非标准目录躲避层规则

## 棕地陷阱（不要复制）

> 当前已知：
> - 早期 `.golangci.yml` 注释引用 `harness/go/specs/...` 旧路径，已迁移至 `.harness/specs/...`，勿按旧路径创建文件。
