# health-nexus 后端 API 契约

> 本文件由门禁自动生成（`tests/api_contract_gen_test.go` → `chi.Walk` 遍历路由树 + `runtime` 反射 handler 源码位置）。
> 每次运行门禁覆盖更新，始终与后端代码同步。前端可据此锁定 handler，进入函数体即见请求体/响应体的 json 解码与写出。

- 生成时间（UTC）：2026-08-01T12:15:08Z
- 端点总数：81

## 鉴权分级说明

| 标记 | 含义 |
|------|------|
| 公开 | 无需认证 |
| 已登录 (任意角色) | 需 `Authorization: Bearer <access>`，任意角色可访问 |
| 医护 (STAFF) | 需 JWT + 医护角色（SUPER_ADMIN/DEPT_ADMIN/DOCTOR/NURSE） |
| 患者 (PATIENT) | 需 JWT + 患者角色 |
| 管理员 (SUPER_ADMIN/DEPT_ADMIN) | 需 JWT + 管理员角色 |

## 端点清单

| # | 方法 | 路径 | 鉴权 | 域 | Handler (源码) |
|---|------|------|------|----|----------------|
| 1 | POST | `/api/auth/change-password` | 已登录 (任意角色) | auth | `health-nexus/internal/domain/auth/handler.(*AuthHandler).ChangePassword` |
| 2 | POST | `/api/auth/login` | 公开 | auth | `health-nexus/internal/domain/auth/handler.(*AuthHandler).UnifiedLogin` |
| 3 | POST | `/api/auth/logout` | 已登录 (任意角色) | auth | `health-nexus/internal/domain/auth/handler.(*AuthHandler).Logout` |
| 4 | POST | `/api/auth/password-reset/confirm` | 公开 | auth | `health-nexus/internal/domain/auth/handler.(*AuthHandler).PasswordResetConfirm` |
| 5 | POST | `/api/auth/password-reset/request` | 公开 | auth | `health-nexus/internal/domain/auth/handler.(*AuthHandler).PasswordResetRequest` |
| 6 | GET | `/api/auth/profile` | 已登录 (任意角色) | auth | `health-nexus/internal/domain/auth/handler.(*AuthHandler).GetProfile` |
| 7 | PATCH | `/api/auth/profile` | 已登录 (任意角色) | auth | `health-nexus/internal/domain/auth/handler.(*AuthHandler).UpdateProfile` |
| 8 | POST | `/api/auth/refresh` | 公开 | auth | `health-nexus/internal/domain/auth/handler.(*AuthHandler).Refresh` |
| 9 | POST | `/api/auth/register` | 公开 | auth | `health-nexus/internal/domain/auth/handler.(*AuthHandler).Register` |
| 10 | GET | `/api/base/departments` | 已登录 (任意角色) | base | `health-nexus/internal/domain/base/handler.(*DepartmentHandler).ListDepartments` |
| 11 | GET | `/api/chat/conversations/` | 已登录 (任意角色) | chat-patient | `health-nexus/internal/domain/chat/handler.(*ConversationHandler).List` |
| 12 | DELETE | `/api/chat/conversations/{id}` | 已登录 (任意角色) | chat-patient | `health-nexus/internal/domain/chat/handler.(*ConversationHandler).Delete` |
| 13 | GET | `/api/chat/conversations/{id}` | 已登录 (任意角色) | chat-patient | `health-nexus/internal/domain/chat/handler.(*ConversationHandler).Get` |
| 14 | PATCH | `/api/chat/conversations/{id}` | 已登录 (任意角色) | chat-patient | `health-nexus/internal/domain/chat/handler.(*ConversationHandler).Patch` |
| 15 | GET | `/api/chat/conversations/{id}/messages` | 已登录 (任意角色) | chat-patient | `health-nexus/internal/domain/chat/handler.(*ConversationHandler).ListMessages` |
| 16 | POST | `/api/chat/messages/{id}/feedback` | 已登录 (任意角色) | chat-patient | `health-nexus/internal/domain/chat/handler.(*ConversationHandler).Feedback` |
| 17 | POST | `/api/chat/stream` | 已登录 (任意角色) | chat-patient | `health-nexus/internal/domain/chat/handler.(*StreamHandler).Stream` |
| 18 | POST | `/api/public/chat/stream` | 公开 | other | `health-nexus/internal/domain/chat/handler.(*StreamHandler).Stream` |
| 19 | GET | `/api/public/departments` | 公开 | other | `health-nexus/internal/domain/base/handler.(*DepartmentHandler).ListPublicDepartments` |
| 20 | GET | `/api/staff/auth/accounts` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | auth-admin | `health-nexus/internal/domain/auth/handler.(*AuthHandler).ListAccounts` |
| 21 | POST | `/api/staff/auth/accounts` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | auth-admin | `health-nexus/internal/domain/auth/handler.(*AuthHandler).CreateAccount` |
| 22 | DELETE | `/api/staff/auth/accounts/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | auth-admin | `health-nexus/internal/domain/auth/handler.(*AuthHandler).SoftDeleteAccount` |
| 23 | POST | `/api/staff/auth/accounts/{id}/lock` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | auth-admin | `health-nexus/internal/domain/auth/handler.(*AuthHandler).LockAccount` |
| 24 | POST | `/api/staff/auth/accounts/{id}/reset-password` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | auth-admin | `health-nexus/internal/domain/auth/handler.(*AuthHandler).ResetAccountPassword` |
| 25 | POST | `/api/staff/auth/accounts/{id}/unlock` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | auth-admin | `health-nexus/internal/domain/auth/handler.(*AuthHandler).UnlockAccount` |
| 26 | GET | `/api/staff/base/departments/` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | base-admin | `health-nexus/internal/domain/base/handler.(*DepartmentHandler).ListTree` |
| 27 | POST | `/api/staff/base/departments/` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | base-admin | `health-nexus/internal/domain/base/handler.(*DepartmentHandler).CreateDepartment` |
| 28 | DELETE | `/api/staff/base/departments/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | base-admin | `health-nexus/internal/domain/base/handler.(*DepartmentHandler).DeleteDepartment` |
| 29 | GET | `/api/staff/base/departments/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | base-admin | `health-nexus/internal/domain/base/handler.(*DepartmentHandler).GetDepartment` |
| 30 | PATCH | `/api/staff/base/departments/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | base-admin | `health-nexus/internal/domain/base/handler.(*DepartmentHandler).UpdateDepartment` |
| 31 | GET | `/api/staff/chat/crisis-events/` | 医护 (STAFF) | chat-staff | `health-nexus/internal/domain/chat/handler.(*CrisisHandler).List` |
| 32 | POST | `/api/staff/chat/crisis-events/{id}/handle` | 医护 (STAFF) | chat-staff | `health-nexus/internal/domain/chat/handler.(*CrisisHandler).Handle` |
| 33 | GET | `/api/staff/config/ai-providers` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).ListAIProviders` |
| 34 | POST | `/api/staff/config/ai-providers` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).CreateAIProvider` |
| 35 | DELETE | `/api/staff/config/ai-providers/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).DeleteAIProvider` |
| 36 | GET | `/api/staff/config/ai-providers/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).GetAIProvider` |
| 37 | PUT | `/api/staff/config/ai-providers/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).UpdateAIProvider` |
| 38 | POST | `/api/staff/config/ai-providers/{id}/test` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).TestAIProvider` |
| 39 | GET | `/api/staff/config/audit-logs` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).ListAuditLogs` |
| 40 | GET | `/api/staff/config/prompts` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).ListPromptTemplates` |
| 41 | POST | `/api/staff/config/prompts` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).CreatePromptTemplate` |
| 42 | GET | `/api/staff/config/prompts/effective` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).GetEffectivePrompt` |
| 43 | DELETE | `/api/staff/config/prompts/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).DeletePromptTemplate` |
| 44 | PUT | `/api/staff/config/prompts/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).UpdatePromptTemplate` |
| 45 | GET | `/api/staff/config/rag` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).GetRAGConfig` |
| 46 | PUT | `/api/staff/config/rag` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).UpdateRAGConfig` |
| 47 | GET | `/api/staff/config/safety-messages` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).GetSafetyMessages` |
| 48 | PUT | `/api/staff/config/safety-messages` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).UpdateSafetyMessages` |
| 49 | GET | `/api/staff/config/safety-policy` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).GetSafetyPolicy` |
| 50 | GET | `/api/staff/config/safety-rules` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).ListSafetyRules` |
| 51 | POST | `/api/staff/config/safety-rules` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).CreateSafetyRule` |
| 52 | DELETE | `/api/staff/config/safety-rules/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).DeleteSafetyRule` |
| 53 | PUT | `/api/staff/config/safety-rules/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).UpdateSafetyRule` |
| 54 | GET | `/api/staff/config/sensitive-words` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).ListSensitiveWords` |
| 55 | POST | `/api/staff/config/sensitive-words` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).CreateSensitiveWord` |
| 56 | DELETE | `/api/staff/config/sensitive-words/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).DeleteSensitiveWord` |
| 57 | PUT | `/api/staff/config/sensitive-words/{id}` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).UpdateSensitiveWord` |
| 58 | GET | `/api/staff/config/status` | 管理员 (SUPER_ADMIN/DEPT_ADMIN) | config | `health-nexus/internal/domain/config/handler.(*ConfigHandler).GetConfigStatus` |
| 59 | GET | `/api/staff/wiki/articles/` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).List` |
| 60 | POST | `/api/staff/wiki/articles/` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).Create` |
| 61 | DELETE | `/api/staff/wiki/articles/{article_id}` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).Delete` |
| 62 | GET | `/api/staff/wiki/articles/{article_id}` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).Get` |
| 63 | PUT | `/api/staff/wiki/articles/{article_id}` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).Update` |
| 64 | POST | `/api/staff/wiki/articles/{article_id}/approve` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).Approve` |
| 65 | POST | `/api/staff/wiki/articles/{article_id}/archive` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).Archive` |
| 66 | GET | `/api/staff/wiki/articles/{article_id}/chunks` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).Chunks` |
| 67 | POST | `/api/staff/wiki/articles/{article_id}/featured` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).SetFeatured` |
| 68 | POST | `/api/staff/wiki/articles/{article_id}/reject` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).Reject` |
| 69 | POST | `/api/staff/wiki/articles/{article_id}/revectorize` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).Revectorize` |
| 70 | POST | `/api/staff/wiki/articles/{article_id}/submit` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).Submit` |
| 71 | POST | `/api/staff/wiki/articles/{article_id}/unarchive` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*StaffArticleHandler).Unarchive` |
| 72 | GET | `/api/staff/wiki/references/` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*ReferenceHandler).List` |
| 73 | POST | `/api/staff/wiki/references/` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*ReferenceHandler).Apply` |
| 74 | GET | `/api/staff/wiki/references/articles` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*ReferenceHandler).ListReferenceableArticles` |
| 75 | DELETE | `/api/staff/wiki/references/{reference_id}` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*ReferenceHandler).Revoke` |
| 76 | POST | `/api/staff/wiki/references/{reference_id}/approve` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*ReferenceHandler).Approve` |
| 77 | POST | `/api/staff/wiki/references/{reference_id}/reject` | 医护 (STAFF) | wiki-staff | `health-nexus/internal/domain/wiki/handler.(*ReferenceHandler).Reject` |
| 78 | GET | `/api/wiki/articles/` | 公开 | wiki-public | `health-nexus/internal/domain/wiki/handler.(*PublicHandler).List` |
| 79 | GET | `/api/wiki/articles/featured` | 公开 | wiki-public | `health-nexus/internal/domain/wiki/handler.(*PublicHandler).Featured` |
| 80 | GET | `/api/wiki/articles/{article_id}` | 公开 | wiki-public | `health-nexus/internal/domain/wiki/handler.(*PublicHandler).Detail` |
| 81 | GET | `/healthz` | 公开 | health | [func1](file:///E:/Codes/health-nexus/backend/tests/api_contract_test.go#L75) |
