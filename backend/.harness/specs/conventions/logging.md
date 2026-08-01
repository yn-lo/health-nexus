---
last_updated: 2026-07-23
status: active
owner: backend-team
---

# 日志规范

## 为什么
结构化日志让日志可被聚合检索、可关联请求（request_id）、可保护 PII（医疗场景下用户名/消息内容是敏感信息）。

## 工具
- **唯一日志库**：标准库 `log/slog`。禁止 `fmt.Print*`、`log.Print*`、`logrus`、`pkg/errors`（depguard + forbidigo 拦截）。
- **初始化**：`internal/platform/logger.New()`，全局使用 `slog` 默认 logger（或经 `slog.WithContext`）。
- **context 传递**：用 `slog.XxxContext(ctx, ...)` 形式，让 request_id 等字段自动注入。

## 日志级别
| 级别 | 何时用 | 示例 |
|------|-------|------|
| `Info` | 关键业务事件（登录成功、文章发布、配置变更） | `slog.InfoContext(ctx, "login success", "user_id", u.ID)` |
| `Warn` | 预期内的失败（登录失败、账户锁定、限流触发） | `slog.WarnContext(ctx, "login failed: user not found", "endpoint", endpoint)` |
| `Error` | 非预期错误（DB/Redis 故障、tryClaim 失败） | `slog.ErrorContext(ctx, "claim refresh token failed", "err", err)` |

> 业务校验失败（401/403/404/422）用 `Warn`，不是 `Error`——它们是预期内的客户端错误，不是系统故障。

## 字段约定
- **键名**：snake_case 字符串字面量（`"user_id"`、`"request_id"`、`"err"`、`"endpoint"`、`"role"`）。
- **错误字段**：统一用 `"err"` 键（`slog.ErrorContext(ctx, "msg", "err", err)`）。
- **context 键**：通过 `internal/shared/contextkeys` 定义（`RequestID`、`UserID`、`Role`、`DeptID`），中间件注入，日志自动携带。

## PII 保护（医疗场景硬性规则）

1. **不记录用户名明文**——登录/认证日志仅记 `user_id` 和 `role`，不记 `username`。
2. **不记录消息内容**——chat 消息日志不记 `content`，仅记 `message_id`/`conversation_id`/`result_code`。
3. **不记录密码/token 明文**——refresh token 日志不记 token 值，仅记操作结果。
4. **不记录完整 API Key**——配置变更日志记 `api_key_masked`（脱敏），不记原始 key。

```go
// ✅ 正确
slog.InfoContext(ctx, "login success", "user_id", u.ID, "role", u.Role)
slog.WarnContext(ctx, "login failed: invalid credentials", "user_id", u.ID)

// ❌ 错误（泄露 PII）
slog.Info("login", "username", username, "password", password)
slog.Error("token", "refresh", refreshToken)
```

## 机械化执行
- `forbidigo`：禁用 `fmt.Print(ln|f)?`、`log.Print(ln|f)?`、`panic`、`os.Exit`（cmd/ 除外）。
- `depguard`：禁用 `log` 标准库、`logrus`、`pkg/errors`。
- 代码审查：检查日志是否携带 context、是否泄露 PII。
