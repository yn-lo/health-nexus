---
last_updated: 2026-07-23
status: active
owner: backend-team
---

# 命名规范

## 为什么
统一命名降低阅读成本，让 AI 与人在任意文件看到一致的约定。Go 社区惯例是基线，项目特例在此显式声明。

## 包与文件
- **包名**：全小写、单数、简短，与目录名一致（`auth`、`user_repo` → 包 `repository`）。域内子包用通用名：`entity`/`repository`/`service`/`handler`。
- **文件名**：snake_case（`auth_service.go`、`chat_send_service_test.go`）。测试文件 `_test.go` 后缀。
- **包注释**：每个包用 `// Package <name> <一句话说明>` 开头（golint/exported 期望）。

## 标识符
| 类型 | 规则 | 示例 |
|------|------|------|
| 导出类型 | PascalCase | `AuthService`、`LoginResponse` |
| 未导出类型/函数 | camelCase | `validateUsername`、`blacklistKey` |
| 接口 | 方法名 + `er` 后缀，或描述能力的名词 | `UserRepo`、`KnowledgeSearcher`、`RedisClient` |
| 常量 | 全大写下划线（项目惯例，非 Go 默认） | `RolePatient`、`passwordMinLen`、`blacklistKeyPrefix` |
| 构造函数 | `New<Type>` | `NewAuthService`、`NewUserRepo` |

## 缩写
缩写词全大写或全小写，不混用（revive `var-naming` 配置：`["ID","URL","HTTP","JSON"]`）。
- ✅ `userID`、`AvatarURL`、`HTTPStatus`、`PrimaryDeptID`
- ❌ `userId`、`AvatarUrl`、`HttpStatus`

## 业务命名约定
- **错误码**：`<DOMAIN>_<REASON>` 大写下划线，如 `AUTH_INVALID_CREDENTIALS`、`WIKI_ARTICLE_NOT_FOUND`。见 `reference/error-codes.md`。
- **角色常量**：`Role<Name>`，如 `RoleDoctor`、`RolePatient`（见 `shared/constants`）。
- **状态常量**：`<Entity>Status<State>`，如 `ArticleStatusPublished`、`ReferenceStatusApproved`。
- **Redis key 前缀**：`<feature>:` 小写，如 `blacklist:refresh:`、`password_reset:`。

## 数据库命名（migrations/*.sql）
- 表名：snake_case 复数（`users`、`article_chunks`、`crisis_events`）。
- 字段名：snake_case（`created_at`、`user_id`、`is_active`）。
- 主键：`id`（BIGSERIAL 或 UUID）。
- 外键：`{关联表单数}_id`（`user_id`、`department_id`）。
- 时间字段：`created_at`、`updated_at`；软删除用 `is_deleted`（BOOLEAN）。
- 索引：`idx_<表>_<列>`（`idx_articles_status_dept`）；唯一索引 `uq_<表>_<列>`。

## 路由
- 公开 API：`/api/<域>/...`（`/api/auth/...`、`/api/wiki/articles`）。
- 医护端：`/api/staff/<域>/...`（`/api/staff/wiki/...`、`/api/staff/chat/...`、`/api/staff/config/...`）。
- 患者端：`/api/chat/...`。
- 资源 ID 用路径参数：`/api/staff/wiki/articles/{id}`。
