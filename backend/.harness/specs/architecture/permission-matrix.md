# 权限矩阵（Permission Matrix）

> 本文档定义系统所有角色、权限边界、路由门禁与数据隔离规则。
> 当新增端点或角色变更时，必须同步更新本文档。

## 一、角色定义

| 角色 | 常量值 | 分组 | 说明 |
|------|--------|------|------|
| 超级管理员 | `SUPER_ADMIN` | Admin / Staff | 跨科室全部权限，系统级配置管理 |
| 科室管理员 | `DEPT_ADMIN` | Admin / Staff | 管理本科室（含子树）文章、人员 |
| 医生 | `DOCTOR` | Staff | 创建/编辑自己的文章 |
| 护士 | `NURSE` | Staff | 创建/编辑自己的文章 |
| 患者 | `PATIENT` | Patient | 患者端使用 |

**角色分组**（定义于 `internal/shared/constants/constants.go`）：

- `IsStaff(role)` → `SUPER_ADMIN | DEPT_ADMIN | DOCTOR | NURSE`（医护入口）
- `IsAdmin(role)` → `SUPER_ADMIN | DEPT_ADMIN`（管理入口）
- `STAFF_ROLES` → `SUPER_ADMIN | DEPT_ADMIN | DOCTOR | NURSE`
- `ADMIN_ROLES` → `SUPER_ADMIN | DEPT_ADMIN`

---

## 二、认证与鉴权架构

```
请求 → JWTAuth(解析JWT) → RequireRole(角色门禁) → DataIsolation(注入DataScope) → Handler → Service(Actor鉴权)
```

| 层 | 机制 | 说明 |
|----|------|------|
| **JWT** | HS256 签名（对称密钥 `HEALTH_NEXUS_JWT_SECRET`），携带 `user_id / role / dept_id / token_type` | access token 鉴权，refresh token 轮换 |
| **中间件 RequireRole** | 路由级角色匹配 | 403 拒绝，先于业务逻辑校验 |
| **中间件 DataIsolation** | 从 ctx 构造 `DataScope{UserID, Role, DeptID}` | 纯采集注入，不做角色判定 |
| **Service Actor** | `Actor{UserID, Role, DeptID}` 传递 | 细粒度科室级鉴权 + 数据过滤 |

---

## 三、API 端点权限表

### 3.1 公开端点（无需认证）

| 方法 | 路径 | 限流 | 说明 |
|------|------|------|------|
| `POST` | `/api/auth/login` | auth | 统一登录 |
| `POST` | `/api/auth/register` | auth_register | 患者注册 |
| `POST` | `/api/auth/refresh` | auth_refresh | 刷新 token |
| `POST` | `/api/auth/password-reset/request` | auth_register | 请求密码重置 |
| `POST` | `/api/auth/password-reset/confirm` | auth_refresh | 确认密码重置 |
| `GET` | `/api/wiki/articles/*` | — | 公开文章详情/列表 |
| `GET` | `/api/public/departments` | — | 科室列表 |
| `POST` | `/api/public/chat/stream` | chat_anon | 匿名对话（X-Device-Id） |
| `GET` | `/healthz` | — | 健康检查 |

### 3.2 任意已认证用户（RequireAnyRole）

| 方法 | 路径 | 额外中间件 | 说明 |
|------|------|-----------|------|
| `POST` | `/api/auth/logout` | — | 登出 |
| `POST` | `/api/auth/change-password` | — | 修改密码 |
| `GET` | `/api/auth/profile` | — | 读取个人资料 |
| `PATCH` | `/api/auth/profile` | — | 更新头像 |
| `POST` | `/api/chat/stream` | rate_limit(chat) | 认证对话 |
| `/api/chat/conversations/*` | — | 对话 CRUD |

### 3.3 医护角色（RequireStaff）

> 所有端点附加 `JWTAuth + DataIsolation`

| 方法 | 路径 | 额外门禁 | 说明 |
|------|------|---------|------|
| `GET` | `/api/staff/wiki/articles` | — | 本科室文章列表（超管可跨科室） |
| `POST` | `/api/staff/wiki/articles` | — | 创建文章（非超管限本科室） |
| `GET/PUT/DELETE` | `/api/staff/wiki/articles/{id}` | — | 文章详情/编辑/删除 |
| `POST` | `/api/staff/wiki/articles/{id}/submit` | — | 提交审核 |
| `POST` | `/api/staff/wiki/articles/{id}/approve` | Admin only | 审核通过（服务层 assertCanReview） |
| `POST` | `/api/staff/wiki/articles/{id}/reject` | Admin only | 审核拒绝（同上） |
| `POST` | `/api/staff/wiki/articles/{id}/featured` | Admin only | 设置热门 |
| `POST` | `/api/staff/wiki/articles/{id}/archive` | — | 归档文章 |
| `POST` | `/api/staff/wiki/articles/{id}/unarchive` | — | 取消归档 |
| `GET` | `/api/staff/wiki/articles/{id}/chunks` | — | 查看文章切片 |
| `POST` | `/api/staff/wiki/articles/{id}/revectorize` | — | 重新切片向量化 |
| `GET` | `/api/staff/wiki/references/` | — | 引用授权列表 |
| `POST` | `/api/staff/wiki/references/` | — | 发起引用申请 |
| `GET` | `/api/staff/wiki/references/articles` | — | 可引用的已发布文章列表 |
| `DELETE` | `/api/staff/wiki/references/{id}` | — | 撤销引用授权 |
| `POST` | `/api/staff/wiki/references/{id}/approve` | Admin only | 批准引用申请 |
| `POST` | `/api/staff/wiki/references/{id}/reject` | Admin only | 拒绝引用申请 |
| `GET` | `/api/staff/chat/crisis-events` | — | 本科室危机事件（超管看全部） |
| `POST` | `/api/staff/chat/crisis-events/{id}/handle` | — | 处理危机事件 |
| `GET` | `/api/base/departments` | — | 科室列表（医护） |

### 3.4 管理员角色（RequireAdmin）

> 所有端点附加 `JWTAuth + DataIsolation`

| 方法 | 路径 | 额外门禁 | 说明 |
|------|------|---------|------|
| `GET` | `/api/staff/auth/accounts` | — | 本科室账户列表（超管看全部） |
| `POST` | `/api/staff/auth/accounts` | — | 创建账户（非超管禁建管理员） |
| `POST` | `/api/staff/auth/accounts/{id}/lock` | — | 锁定账户 |
| `POST` | `/api/staff/auth/accounts/{id}/unlock` | — | 解锁账户 |
| `DELETE` | `/api/staff/auth/accounts/{id}` | SUPER_ADMIN only | 软删除用户 |
| `POST` | `/api/staff/auth/accounts/{id}/reset-password` | SUPER_ADMIN only | 重置密码 |
| `GET/POST` | `/api/staff/base/departments/*` | — | 科室树 CRUD |
| `GET/POST` | `/api/staff/config/*` | — | 系统配置 CRUD |

---

## 四、Service 层权限规则

### 4.1 Wiki 域（文章管理）

| 操作 | 超级管理员 | 科室管理员 | 医生/护士 |
|------|-----------|-----------|----------|
| 创建文章 | 任意科室 | 限本科室 | 限本科室 |
| 列出文章 | 全部/指定科室 | 自动过滤本科室 | 自动过滤本科室 |
| 编辑文章 | 任意 | 仅作者本人 | 仅作者本人 |
| 删除文章 | 任意 | 仅作者本人 | 仅作者本人 |
| 审核文章 | 全部可审 | 仅本科室 | **无权** |
| 设置热门 | 全部 | 仅本科室 | **无权** |
| 引用申请 | 任意目标科室 | 仅目标为本科室 | 仅目标为本科室 |
| 引用审批/撤销 | 任意 | 仅本科室 | **无权** |

关键鉴权函数（`article_service.go`）：
- `assertCanManage(a, actor)` — 超管 OR 作者 OR 同科室
- `assertCanAuthorOrAdmin(a, actor)` — 超管 OR 作者本人
- `assertCanFeature(a, actor)` — 管理员 + 同科室（超管豁免）
- `assertCanReview(a, actor)` — 管理员 + 同科室（超管豁免）

### 4.2 Auth 域（账户管理）

| 操作 | 超级管理员 | 科室管理员 |
|------|-----------|-----------|
| 列出账户 | 全部 | 仅本科室 |
| 创建 PATIENT/DOCTOR/NURSE | ✅ | ✅ |
| 创建 SUPER_ADMIN/DEPT_ADMIN | ✅ | **禁止（403）** |
| 锁定/解锁 PATIENT/DOCTOR/NURSE | ✅ | ✅ |
| 锁定/解锁管理员角色 | ✅ | **禁止（403）** |
| 软删除用户 | ✅ | **禁止（403）** |
| 重置密码 | ✅ | **禁止（403）** |

### 4.3 Chat 域（危机事件）

| 操作 | 超级管理员 | 科室管理员 | 医生/护士 |
|------|-----------|-----------|----------|
| 列出危机事件 | 全部 | 仅本科室（按会话锁定科室过滤） | 仅本科室 |
| 处理危机事件 | ✅ | ✅ | ✅ |

### 4.4 Config 域（系统配置）

全局配置，仅管理员可管理。所有端点需 `JWTAuth + RequireAdmin + DataIsolation`。
服务层不做科室过滤（系统配置为全局单例）。

---

## 五、数据隔离规则

### 5.1 科室数据范围

非超管用户的科室范围由其 JWT 中 `dept_id`（主科室）决定：
- `DEPT_ADMIN`：可见 `dept_id` 科室及其子树的数据
- `DOCTOR / NURSE`：可见 `dept_id` 科室的数据

当前实现：`dept_id` 取自 `user_departments.is_primary = TRUE` 的行。

### 5.2 各域隔离状态

| 域 | 资源 | 隔离实现 | 状态 |
|----|------|---------|------|
| Wiki | 文章列表/审核 | Service 层 Actor.DeptID 过滤 | ✅ |
| Wiki | 文章创建 | Service 层跨科拒绝 | ✅ |
| Wiki | 跨科室引用 | Service 层目标科室校验 | ✅ |
| Chat | 危机事件列表 | Service 层 Actor.DeptID 过滤 | ✅ |
| Auth | 账户列表 | Service 层 Actor.DeptID 过滤 | ✅ |
| Chat | 对话/患者数据 | 待实现 | ❌ |

---

## 六、前端路由守卫

| 守卫函数 | 允许角色 | 失败行为 |
|---------|---------|---------|
| `staffRouteGuard` | STAFF_ROLES | 患者 → `/chat`；未登录 → `/login` |
| `adminRouteGuard` | ADMIN_ROLES | 普通医护 → `staff-dashboard` |
| `superAdminRouteGuard` | SUPER_ADMIN | DEPT_ADMIN → `staff-dashboard` |
| `patientRouteGuard` | PATIENT | 医护 → `/staff`（含 `allowStaffPreview` 例外） |

定义于 [frontend/src/shared/utils/route-guard.ts](file:///e:/Codes/health-nexus/frontend/src/shared/utils/route-guard.ts)。

---

## 七、变更记录

| 日期 | 变更 | 作者 |
|------|------|------|
| 2026-07-30 | 初始版本，补充危机事件与账户管理科室隔离 | — |
