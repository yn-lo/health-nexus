---
last_updated: 2026-07-30
status: active
owner: backend-team
---

# 数据流

描述请求从入口到存储的完整路径，帮助理解数据如何在各层流转。

## 1. 启动装配流

```
cmd/server/main.go
  └─ config.Load()                      读取 config.yaml + 环境变量覆盖
  └─ di.NewApp(ctx, cfg)
       ├─ NewInfrastructure()           postgres pool / redis / asynq / txmgr / locker / rateLimiter / auth
       ├─ base 域                       deptRepo → deptSvc → deptHandler
       ├─ auth 域                       userRepo + tokenIssuer → authSvc → authHandler
       ├─ config 域                     7 个 repo → configSvc（含 AES key）→ configH
       ├─ wiki 域                       4 个 repo + adapter → articleSvc / referenceSvc → wikiRouter
       └─ chat 域                       adapter 桥接 + LLM 客户端(DB 加载) → rag 组件 → chatSvc → chatRouter
  └─ buildRouter(app)                   chi 装配中间件 + 5 域路由 + 404/405
  └─ srv.ListenAndServe                 graceful shutdown on SIGTERM
```

**关键点**：LLM 客户端从 DB active provider 动态加载（方案 C），DB 配置变更通过 Redis Pub/Sub 通知热切换（`adapter.ReloadAndSwap`），无需重启服务。

## 2. 同步请求流（以医护登录为例）

```
POST /api/auth/login
  → middleware: RequestID → Recover → RequestLog → CORS
  → AuthHandler.UnifiedLogin
       ├─ decodeJSON(r, &req)           严格模式，空体→422，格式错→422，1MB 限制
       ├─ AuthService.UnifiedLogin(ctx, username, password)
       │    ├─ repo.GetByUsername(ctx, username)        SQL 查询 + LEFT JOIN user_departments
       │    │    └─ 未找到 → (nil, nil) → service 返回 401 AUTH_INVALID_CREDENTIALS（不泄露存在性）
       │    ├─ user.IsLocked()                          → 423 AUTH_ACCOUNT_LOCKED
       │    ├─ crypto.ComparePasswordAndPassword        → 401 AUTH_INVALID_CREDENTIALS
       │    └─ tokenIssuer.Issue(ctx, id, role, deptID) → access + refresh
       └─ response.WriteOK(w, LoginResponse{...})  JSON {access, refresh, user}
```

**错误流**：service 返回 `*AppError`，handler 经 `response.WriteError` 提取 HTTP+Code；未知错误统一 500 + 日志（含 request_id）。

## 3. 认证与角色门禁流

```
请求 → JWTAuth 中间件
         ├─ 解析 Authorization: Bearer <token>
         ├─ auth.Parse(token)            验签(HS256) + 校验 issuer/expiry/token_type=access
         ├─ 注入 ctx: UserID / Role / DeptID（contextkeys）
         └─ 失败 → 401 UNAUTHORIZED
     → RequireRole / RequireAdmin
         ├─ 从 ctx 取 Role
         ├─ config 域: constants.IsAdmin(role)   否则 403
         └─ chat 聊天端: 任意已认证角色（RequireAnyRole）
         └─ chat 医护端: constants.IsStaff(role)  否则 403
```

## 4. SSE 流式对话流（chat 域）

```
POST /api/chat/stream  {message, conversation_id, selected_dept_id?}
  → JWTAuth + 限流 + 角色校验(RequireAnyRole)
  → StreamHandler
       ├─ ChatSendService.Stream(ctx, in, out)
       │    ├─ locker.Lock(conversation_id)        会话级互斥锁（Redis，流程最前置；失败→409 CHAT_CONCURRENT_STREAM）
       │    ├─ dept.ResolveForPatient(deptID)      科室范围校验（selected_dept_id=0「全部科室」归一化为未指定）
       │    ├─ loadOrPrepareConversation(...)      会话加载/创建
       │    ├─ EmergencyCheck(content)             紧急症状预提醒（不阻断）
       │    ├─ CheckRules(content)                 规则层安全审查（敏感词 + 注入）
       │    │    └─ 命中危机 → 写 crisis_events + 返回 CRISIS 结果
       │    ├─ llm.IsReady()                       LLM 就绪性预检
       │    ├─ LLMCheck(content)                   LLM 深度审查（疑似复核）
       │    ├─ rewriter.Rewrite(query)             查询改写（可选，降级为原始查询）
       │    ├─ knowledgeSearcher.Search(ctx, q)    向量(pgvector) + BM25(tsvector) 混合 + 可选 rerank
       │    ├─ promptProvider.GetSystemPrompt(ctx)  系统提示词（DB 配置 → 硬编码兜底）
       │    ├─ llmClient.Stream(ctx, msgs, chunks) SSE 流式生成
       │    ├─ outputSafety.Validate(assistant)    输出安全审查（replace/append；diagnosisRe 豁免「诊断为准」）
       │    └─ repo 持久化 conversation + message + referenced_chunks（事务）
       └─ 逐 chunk flush 到客户端（WriteTimeout=0，由 ctx deadline 控制）
```

**关键点**：server 级 `WriteTimeout=0`（SSE 持续数分钟），超时由 handler ctx deadline + `chatPendingLockTTL` 5min 控制。

**并发锁语义**：`locker.Lock` 是 Stream 的第一步，先于任何 conversation 事件与持久化。锁竞争失败（`redis.ErrLockNotAcquired`）直接返回 `409 CHAT_CONCURRENT_STREAM`（Conflict，客户端可据此重试）——若先下发 conversation 事件再失败，错误会降级为 SSE error 事件（HTTP 200），客户端无从重试。故 conversation 加载/创建事件一律移至获锁成功之后。

**输出审查**：`outputSafety.Validate` 的 `diagnosisRe` 对安全话术「诊断为准」（如「以临床诊断为准」）设例外，避免把引导就医的合规表述误判为越权诊断而替换。

## 5. 异步任务流（worker）

```
wiki 文章状态变更（提交/审核/重排）
  → ArticleService 检测内容变更（content_hash）
  → adapter.NewAsynqVectorizeEnqueuer.Enqueue(articleID)
       └─ asynq 任务入队 Redis
  → cmd/worker/asynq.Serve（3 个 task handler）
       ├─ TaskVectorizeArticle → VectorizeHandler.HandleVectorize
       │    ├─ chunkRepo 加载切片
       │    ├─ embedClient.Embed(chunks)          向量化（依赖 LLM Embedding）
       │    └─ chunkRepo.UpdateEmbedding          写回 pgvector
       ├─ TaskReviewOverdueScan                   每日 03:00 复审逾期扫描（Scheduler 触发）
       └─ TaskReviewNotify                        单条复审通知（slog 占位）
```

**关键点**：worker 通过 `adapter.BuildSwappableClients()` + `adapter.ReloadAndSwap()` 独立创建 Embedding 客户端（不依赖 chat LLM），同样支持 Redis Pub/Sub 热切换。

## 6. 事务边界

- **唯一事务边界在 service 层**，经 `platform/postgres.TxManager.WithTx(ctx, fn)`。
- repository 方法接收 `ctx`，在事务内时自动使用事务连接（`TxManager` 通过 ctx 注入 `pgx.Tx`）。
- handler 不感知事务，repository 不开启事务。

## 7. 配置覆盖流

```
config.yaml（默认值）
  ↑ viper.SetEnvPrefix("HEALTH_NEXUS")
  ↑ 环境变量 HEALTH_NEXUS_<SECTION>_<KEY>（自动绑定，如 HEALTH_NEXUS_LLM_API_KEY）
  ↑ 环境变量优先级最高
```

config 域的运行时配置（AI provider/安全规则等）存储在 PostgreSQL，启动时由 `adapter.ReloadAndSwap` 加载。
