---
last_updated: 2026-07-23
status: active
owner: backend-team
---

# 错误处理规范

## 为什么
统一错误模型让 handler 能机械地提取 HTTP 状态码与业务错误码，让客户端获得一致的 `{code, message}` 响应，让日志能追溯原始错误而不泄露给客户端。

## 核心模型：AppError

所有业务错误必须是 `*apperrors.AppError`（`internal/shared/errors`），携带：
- `Code`：业务错误码（`AUTH_INVALID_CREDENTIALS`），见 `reference/error-codes.md`
- `Message`：用户可读消息（中文，不暴露内部细节）
- `HTTP`：HTTP 状态码
- `Cause`：原始错误（仅日志记录，不序列化到响应）

## 各层职责

| 层 | 做什么 | 不做什么 |
|----|--------|---------|
| **repository** | 返回 `(nil, nil)` 表示未找到；用 `fmt.Errorf("...: %w", err)` 包装底层错误 | 不构造 `*AppError`（那是 service 的决策） |
| **service** | 将数据层结果映射为 `*AppError`（如 `u == nil` → 401/404）；用 `fmt.Errorf` 包装非业务错误 | 不直接写 HTTP 响应；不 `panic` |
| **handler** | 调 service → `response.WriteError(w, r, err)`；仅做请求级校验（字段缺失→422） | 不重复 service 的业务判断 |

## 标准用法

### service 返回业务错误
```go
// ✅ 用构造器
if u == nil {
    return nil, apperrors.NewAppError("AUTH_INVALID_CREDENTIALS", "用户名或密码错误", apperrors.StatusUnauthorized, nil)
}
// ✅ 或语义构造器
return apperrors.NotFound("WIKI_ARTICLE_NOT_FOUND", "文章不存在")
```

### service 包装非业务错误（DB/Redis 故障）
```go
// ✅ fmt.Errorf + %w，不构造 AppError（让 handler 走 500 兜底）
u, err := s.repo.GetByUsername(ctx, username)
if err != nil {
    return nil, fmt.Errorf("get user: %w", err)
}
```

### handler 统一写错误响应
```go
// ✅ 调 service 后用 WriteError
res, err := h.svc.UnifiedLogin(r.Context(), req.Username, req.Password)
if err != nil {
    response.WriteError(w, r, err) // AppError 提取 HTTP+Code，未知错误统一 500
    return
}
```

## HTTP 状态码语义

| 状态码 | 含义 | 何时用 | 状态常量 |
|--------|------|-------|---------|
| 400 | 请求格式合法但语义错误 | 用户名格式/密码强度不足 | `StatusBadRequest` |
| 401 | 未认证 / 凭证无效 | 无 token、token 过期、密码错误（不泄露存在性） | `StatusUnauthorized` |
| 403 | 已认证但无权限 | 角色不匹配、跨科室访问 | `StatusForbidden` |
| 404 | 资源不存在 | 文章/会话不存在 | `StatusNotFound` |
| 409 | 冲突 | 用户名已存在 | `StatusConflict` |
| 422 | 请求体校验失败 | 字段缺失、JSON 格式错误 | `StatusUnprocessableEntity` |
| 423 | 账户锁定 | 登录/刷新时账户 is_locked | `StatusLocked` |
| 429 | 限流 | 超过速率限制 | `StatusTooManyRequests` |
| 500 | 服务器内部错误 | 未知错误（仅日志，响应 `INTERNAL_ERROR`） | `StatusInternalServerError` |
| 503 | 服务暂不可用 | Redis/DB 故障（fail-closed） | `StatusServiceUnavailable` |

## 安全规则（不可违反）

1. **登录失败统一返回 401 `AUTH_INVALID_CREDENTIALS`**——用户不存在与密码错误不可区分，避免泄露用户名是否存在。
2. **密码重置请求始终返回成功**——用户不存在也返回 nil，不泄露存在性。
3. **未知错误响应固定 `INTERNAL_ERROR` + "服务器内部错误"**——原始错误仅入日志（含 request_id），不进响应体。
4. **refresh token 失效统一 401**——不区分"已登出/已轮换/已过期"，统一 `AUTH_INVALID_REFRESH`。
5. **Redis 故障 fail-closed**——refresh 轮换/登出在 Redis 不可用时返回 503，不可静默放行（防止 token 复用攻击窗口）。

## 机械化执行
- `errcheck`（AC-ARCH-08）：错误必须显式处理，`_ = err` 由 gosec G104 覆盖。
- `internal/harness/arch/arch_test.go`：service 不得 import `net/http`（用 `shared/errors` 状态码常量替代 `net/http.StatusXxx`）。
- API 契约测试：验证 401/403/404/405 响应体含 `{code, message}`。
