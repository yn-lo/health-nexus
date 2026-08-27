---
last_updated: 2026-07-24
status: active
owner: backend-team
---

# 错误码参考

## 规则
- 错误码格式：`<DOMAIN>_<REASON>`，大写下划线。
- 错误码按模块分段，每段预留充足编号空间（无需数字编号，按域前缀归类）。
- **新增业务错误必须在本文件登记**，并在测试中覆盖对应错误分支。
- 通用错误（无域前缀）用于跨域复用场景，避免重复定义。
- 错误码与 HTTP 状态码的映射见 `conventions/error-handling.md`。

## 通用错误（无域前缀）
| 错误码 | HTTP | 说明 | 来源 |
|--------|------|------|------|
| `VALIDATION_MISSING` | 422 | 请求体字段缺失 | handler decodeJSON |
| `VALIDATION_INVALID` | 422 | 请求体格式错误 | handler decodeJSON |
| `VALIDATION_INVALID_PAGE` | 422 | 分页参数无效 | shared/pagination |
| `VALIDATION_INVALID_BOOL` | 422 | bool 参数无效 | chat handler |
| `VALIDATION_INVALID_LIMIT` | 422 | limit 参数无效 | chat handler |
| `UNAUTHORIZED` | 401 | 未认证 | auth service / middleware |
| `FORBIDDEN` | 403 | 角色无权访问 | middleware/require_role |
| `NOT_FOUND` | 404 | 资源不存在（统一 404） | main.buildRouter |
| `METHOD_NOT_ALLOWED` | 405 | 请求方法不允许 | main.buildRouter |
| `RATE_LIMITED` | 429 | 超过速率限制 | middleware/rate_limit |
| `RATE_LIMITER_UNAVAILABLE` | 503 | 限流服务暂不可用 | middleware/rate_limit |
| `INTERNAL_ERROR` | 500 | 服务器内部错误（兜底） | shared/response |

## AUTH 模块（认证）
| 错误码 | HTTP | 说明 |
|--------|------|------|
| `AUTH_INVALID_CREDENTIALS` | 401 | 用户名或密码错误（不泄露存在性） |
| `AUTH_ACCOUNT_LOCKED` | 423 | 账户已锁定 |
| `AUTH_NOT_STAFF` | 403 | 非医护角色禁止访问医护端 |
| `AUTH_NOT_PATIENT` | 403 | 非患者角色禁止访问患者端 |
| `AUTH_USERNAME_EXISTS` | 409 | 用户名已存在 |
| `AUTH_USERNAME_INVALID` | 422 | 用户名格式不合法（长度/字符） |
| `AUTH_PASSWORD_WEAK` | 422 | 密码强度不足 |
| `AUTH_INVALID_REFRESH` | 401 | refresh token 无效/已失效/类型错误 |
| `AUTH_TOKEN_MISMATCH` | 403 | refresh token 与当前用户不匹配 |
| `AUTH_REFRESH_UNAVAILABLE` | 503 | 刷新服务暂不可用（Redis 故障 fail-closed） |
| `AUTH_BLACKLIST_UNAVAILABLE` | 503 | 登出服务暂不可用（Redis 故障 fail-closed） |
| `AUTH_RESET_UNAVAILABLE` | 503 | 密码重置服务暂不可用 |
| `AUTH_RESET_TOKEN_INVALID` | 400 | 重置链接无效或已过期 |
| `AUTH_USER_NOT_FOUND` | 404 | 用户不存在 |
| `AUTH_OLD_PASSWORD_WRONG` | 401 | 原密码错误（修改密码时校验） |
| `AUTH_AVATAR_TOO_LONG` | 422 | 头像地址超过 2048 字符 |
| `AUTH_ROLE_INVALID` | 400 | 角色无效（非系统定义的合法角色） |
| `AUTH_FORBIDDEN_ROLE` | 403 | 无权创建/操作管理员账户（非 SUPER_ADMIN） |
| `AUTH_SELF_LOCK` | 409 | 不能锁定或解锁自己的账户 |
| `AUTH_UNAUTHORIZED` | 401 | 未认证（ctx 中缺少用户身份） |

## BASE 模块（科室）
| 错误码 | HTTP | 说明 |
|--------|------|------|
| `BASE_DEPT_NOT_FOUND` | 404 | 科室不存在 |
| `BASE_DEPT_NAME_REQUIRED` | 422 | name 不能为空 |
| `BASE_DEPT_NAME_TOO_LONG` | 422 | name 长度需为 1-100 字符 |
| `BASE_DEPT_PARENT_NOT_FOUND` | 400 | 父科室不存在 |
| `BASE_DEPT_CYCLE` | 409 | 不能将科室移动到自身或其子科室下 |
| `BASE_DEPT_HAS_CHILDREN` | 409 | 该科室仍有子科室 |
| `BASE_DEPT_HAS_USERS` | 409 | 该科室仍有用户关联 |
| `BASE_DEPT_OUT_OF_SCOPE` | 403 | 仅可管理本科室子树内的科室 |
| `BASE_DEPT_EMPTY_UPDATE` | 422 | 至少需要一个字段 |
| `BASE_DEPT_INVALID_ID` | 422 | id 参数缺失/无效 |
| `BASE_DEPT_EMPTY_BODY` | 422 | 请求体不能为空 |
| `BASE_DEPT_INVALID_JSON` | 422 | 请求体格式错误 |
| `BASE_INVALID_ACTIVE` | 400 | active 参数无效 |

## WIKI 模块（知识库）
| 错误码 | HTTP | 说明 |
|--------|------|------|
| `WIKI_ARTICLE_NOT_FOUND` | 404 | 文章不存在或未发布 |
| `WIKI_TITLE_REQUIRED` | 422 | title 不能为空 |
| `WIKI_CONTENT_REQUIRED` | 422 | content 不能为空 |
| `WIKI_DEPT_REQUIRED` | 422 | department_id 不能为空 |
| `WIKI_DEPT_FORBIDDEN` | 403 | 只能在本科室创建文章 |
| `WIKI_FORBIDDEN` | 403 | 无权操作该文章 |
| `WIKI_SELF_REVIEW_FORBIDDEN` | 403 | 不能审核自己的文章（已废弃，管理员可自审） |
| `WIKI_DEPT_MISMATCH` | 403 | 非本科室文章不可审核 |
| `WIKI_REVIEW_FORBIDDEN` | 403 | 仅管理员可审核文章 |
| `WIKI_ARCHIVED_READONLY` | 409 | 归档文章不可修改 |
| `WIKI_INVALID_STATUS` | 409 | 文章状态非预期，无法操作 |
| `WIKI_INVALID_STATUS_PARAM` | 422 | status 参数无效 |
| `WIKI_NOT_PUBLISHED` | 409 | 仅已发布文章可重新切片 |
| `WIKI_REASON_REQUIRED` | 422 | reason 不能为空 |
| `WIKI_VECTORIZE_UNAVAILABLE` | 503 | 向量化服务未配置 |
| `WIKI_VECTORIZE_ENQUEUE_FAILED` | 503 | 入队失败 |
| `WIKI_ARTICLE_NO_DEPT` | 400 | 文章未归属任何科室 |
| `WIKI_REF_NOT_FOUND` | 404 | 引用授权记录不存在 |
| `WIKI_REF_ARTICLE_REQUIRED` | 422 | article_id 无效 |
| `WIKI_REF_TARGET_DEPT_REQUIRED` | 422 | target_dept_id 无效 |
| `WIKI_REF_ARTICLE_NOT_PUBLISHED` | 400 | 仅已发布文章可发起引用申请 |
| `WIKI_REF_NOT_ALLOWED` | 400 | 该文章未开放引用授权 |
| `WIKI_REF_APPLICANT_DEPT` | 403 | 只能为本科室发起引用申请 |
| `WIKI_REF_SAME_DEPT` | 400 | 源科室与目标科室相同 |
| `WIKI_REF_SOURCE_DEPT_MISSING` | 400 | 源科室不存在 |
| `WIKI_REF_PENDING_EXISTS` | 409 | 已存在待审核的引用申请 |
| `WIKI_REF_INVALID_STATUS` | 409 | 引用状态非预期 |
| `WIKI_REF_INVALID_STATUS_PARAM` | 422 | status 参数无效 |
| `WIKI_REF_INVALID_DIRECTION` | 422 | direction 参数无效 |
| `WIKI_REF_REASON_REQUIRED` | 422 | reason 不能为空 |
| `WIKI_REF_SELF_REVIEW` | 403 | 不得审核自己发起的引用申请 |
| `WIKI_REF_REVIEW_FORBIDDEN` | 403 | 仅科室管理员可操作引用授权 |
| `WIKI_REF_DEPT_MISMATCH` | 403 | 仅源科室管理员可操作 |
| `WIKI_INVALID_ARTICLE_ID` | 400 | article_id 路径参数缺失/无效 |
| `WIKI_INVALID_REFERENCE_ID` | 400 | reference_id 路径参数缺失/无效 |
| `WIKI_EMPTY_BODY` | 422 | 请求体不能为空 |
| `WIKI_INVALID_JSON` | 422 | 请求体格式错误（严格模式拒绝未知字段） |

## CHAT 模块（对话）
| 错误码 | HTTP | 说明 |
|--------|------|------|
| `CHAT_CONVERSATION_NOT_FOUND` | 404 | 会话不存在或不属于当前用户 |
| `CHAT_CRISIS_NOT_FOUND` | 404 | 危机事件不存在 |
| `CHAT_INVALID_ID` | 400 | id 参数缺失/格式错误 |
| `CHAT_INVALID_CONVERSATION_ID` | 400 | conversation_id 格式错误 |
| `CHAT_INVALID_DEPT_ID` | 400 | selected_dept_id 格式错误 |
| `CHAT_INVALID_BEFORE` | 400 | before 格式错误 |
| `CHAT_CRISIS_LEVEL_INVALID` | 400 | level 仅允许 high\|medium\|low |
| `CHAT_MESSAGE_EMPTY` | 400 | 消息内容不能为空 |
| `CHAT_PATCH_EMPTY` | 422 | 至少需要修改一个字段 |
| `CHAT_PATCH_BODY_INVALID` | 422 | 请求体格式错误 |
| `CHAT_CRISIS_BODY_INVALID` | 422 | 请求体格式错误 |
| `CHAT_MESSAGE_TOO_LONG` | 422 | 消息长度超过 2000 字符 |
| `CHAT_CONCURRENT_STREAM` | 409 | 会话正在生成中，请稍后重试 |
| `CHAT_CRISIS_ALREADY_HANDLED` | 409 | 危机事件已处理 |
| `CHAT_CRISIS_EVENT_EXISTS` | 409 | 会话包含危机事件记录，不可删除 |
| `CHAT_DEPT_LOCKED` | 409 | 会话科室已锁定，不可更改 |
| `CHAT_LLM_UNAVAILABLE` | 503 | AI 服务暂不可用 |
| `CHAT_LLM_TIMEOUT` | 503 | AI 服务响应超时 |

## CONFIG 模块（系统配置）

### AI Provider 提供商
| 错误码 | HTTP | 说明 |
|--------|------|------|
| `CONFIG_INVALID_PROVIDER_TYPE` | 422 | provider_type 无效 |
| `CONFIG_NAME_REQUIRED` | 422 | name 不能为空 |
| `CONFIG_API_URL_REQUIRED` | 422 | api_url 不能为空 |
| `CONFIG_MODEL_NAME_REQUIRED` | 422 | model_name 不能为空 |
| `CONFIG_API_KEY_REQUIRED` | 422 | api_key 不能为空 |
| `CONFIG_EMBEDDING_DIM_REQUIRED` | 422 | embedding 提供商必须声明 dimension |
| `CONFIG_AI_PROVIDER_DUPLICATE` | 409 | AI 提供商名称已存在 |
| `CONFIG_AI_PROVIDER_NOT_FOUND` | 404 | AI 提供商不存在 |
| `CONFIG_EMBEDDING_DIM_CHANGE_BLOCKED` | 409 | 已有切片向量时禁止变更 embedding 维度 |

### 敏感词与安全规则
| 错误码 | HTTP | 说明 |
|--------|------|------|
| `CONFIG_WORD_REQUIRED` | 422 | word 不能为空 |
| `CONFIG_SENSITIVE_WORD_DUPLICATE` | 409 | 同类别下敏感词已存在 |
| `CONFIG_SENSITIVE_WORD_NOT_FOUND` | 404 | 敏感词不存在 |
| `CONFIG_INVALID_CATEGORY` | 422 | category 无效 |
| `CONFIG_PATTERN_REQUIRED` | 422 | pattern 不能为空 |
| `CONFIG_PATTERN_TOO_LONG` | 422 | pattern 长度不能超过 500 字符 |
| `CONFIG_INVALID_PATTERN` | 422 | pattern 不是合法正则 |
| `CONFIG_INVALID_ACTION` | 422 | action 无效，必须为 replace 或 block |
| `CONFIG_REPLACEMENT_REQUIRED` | 422 | action=replace 时 replacement 必填 |
| `CONFIG_REPLACEMENT_TOO_LONG` | 422 | replacement 长度不能超过 500 字符 |
| `CONFIG_SAFETY_RULE_DUPLICATE` | 409 | 安全规则名称已存在 |
| `CONFIG_SAFETY_RULE_NOT_FOUND` | 404 | 安全规则不存在 |

### RAG 参数
| 错误码 | HTTP | 说明 |
|--------|------|------|
| `CONFIG_RAG_CHUNK_SIZE_RANGE` | 422 | chunk_size 范围 200-2000 |
| `CONFIG_RAG_CHUNK_OVERLAP_RANGE` | 422 | chunk_overlap 范围 0-500 |
| `CONFIG_RAG_OVERLAP_TOO_LARGE` | 422 | chunk_overlap 必须小于 chunk_size |
| `CONFIG_RAG_MAX_CHUNKS_RANGE` | 422 | max_chunks 范围 1-50 |
| `CONFIG_RAG_TOP_K_RANGE` | 422 | top_k 范围 1-50 |
| `CONFIG_RAG_SIMILARITY_RANGE` | 422 | similarity_threshold 范围 0.0-1.0 |
| `CONFIG_RAG_RERANK_THRESHOLD_RANGE` | 422 | rerank_threshold 范围 0.0-1.0 |
| `CONFIG_RAG_OOD_THRESHOLD_RANGE` | 422 | ood_threshold 范围 0.0-0.5 |

### Prompt 模板
| 错误码 | HTTP | 说明 |
|--------|------|------|
| `CONFIG_INVALID_PROMPT_TYPE` | 422 | type 无效 |
| `CONFIG_CONTENT_REQUIRED` | 422 | content 不能为空 |
| `CONFIG_EMPTY_UPDATE` | 422 | 至少需要一个字段：content 或 is_active |
| `CONFIG_PROMPT_VERSION_CONFLICT` | 409 | 版本冲突，请重试 |
| `CONFIG_PROMPT_NOT_FOUND` | 404 | Prompt 模板不存在 |

### 通用
| 错误码 | HTTP | 说明 |
|--------|------|------|
| `CONFIG_INVALID_ENTITY_TYPE` | 422 | entity_type 无效（审计日志） |
| `CONFIG_INVALID_ID` | 422 | id 参数无效 |
| `CONFIG_INVALID_BOOL` | 422 | bool 参数无效 |
| `CONFIG_EMPTY_BODY` | 422 | 请求体不能为空 |
| `CONFIG_INVALID_JSON` | 422 | 请求体格式错误 |
