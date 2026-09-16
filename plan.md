整体判断：**分层方向合理，但对话链条还没有形成可靠的生命周期闭环。** 主要问题集中在会话身份、生成状态、前后端同步和安全审查时序，而不是缺少更多抽象层。

这次沿着“页面发送 → SSE → 会话/锁 → 安全检查 → 历史与检索 → 生成 → 持久化 → 历史恢复”审查。以下按优先级排列；标注“已复现”的问题用临时测试验证过。

**优先修复的 6 个问题**

1. **[P1] 输出安全审查发生在危险内容已经发送之后。已复现。**  
   [chat_send_service.go:587](E:/Codes/health-nexus/backend/internal/domain/chat/service/chat_send_service.go:587) 先发送原始 token，整个回答结束后才执行 `Validate`。复现中，“建议立即停药”先通过 SSE 发出，随后才被替换。用户可能已经阅读、复制，断线时还可能保留原文。  
   应在内容发送前审查；可按句缓冲审查，或对高风险回答采用完整审核后输出。

2. **[P1] 输出过滤命中第一条替换规则就返回，其他违规内容会留下。已复现。**  
   [safety_output.go:195](E:/Codes/health-nexus/backend/internal/shared/rag/safety_output.go:195) 替换后立即 `return`。输入同时包含停药、诊断和延误就医表述时，只替换停药部分，另外两项仍进入最终答案和数据库。  
   替换型规则应继续检查剩余内容，全部检查结束后再返回。

3. **[P1] 新会话首轮与后续请求使用不同锁键，可绕过同会话互斥。已复现。**  
   [chat_send_service.go:121](E:/Codes/health-nexus/backend/internal/domain/chat/service/chat_send_service.go:121) 创建会话后仍用原始 `in` 构造锁键：首轮是 `chat_pending:user:new`，拿到 SSE 返回的会话 ID 后，第二个请求使用 `chat_pending:user:uuid`。两个请求可以同时生成，造成历史读取和消息顺序交错。  
   应统一使用已经解析出的 `sess.ID()` 加锁。代码注释声称已经这样处理，实际实现没有落实。

4. **[P1] 指定科室的历史会话，刷新或重新打开后无法正常续聊。**  
   [ChatConversation.vue:91](E:/Codes/health-nexus/frontend/src/chat/views/ChatConversation.vue:91) 只从 URL query 初始化科室，缺省为 `0`；历史恢复只加载消息，没有加载会话的 `locked_dept_id`。随后发送 `selected_dept_id=0`，后端会以 `CHAT_DEPT_LOCKED` 返回 409。  
   应在打开会话时恢复会话详情；已有会话的请求也可以省略科室参数，由服务端采用锁定值。

5. **[P1] 匿名“新对话”没有真正创建新会话，删除也不会清除模型上下文。已复现会话 ID 复用。**  
   [chat_send_service.go:180](E:/Codes/health-nexus/backend/internal/domain/chat/service/chat_send_service.go:180) 仅按设备 ID 派生会话 ID，忽略传入的 `conversation_id`。前端新建只是清空界面，后端继续读取旧 Redis 历史；本地缓存还会用同一 ID 覆盖旧记录。  
   应把设备身份与会话身份分开：一个设备可以拥有多个会话，新建、续聊、删除需要一致的服务端语义。

6. **[P2] 流中断后的半截答案被保存成 `ANSWERED`。已复现。**  
   [chat_send_service.go:575](E:/Codes/health-nexus/backend/internal/domain/chat/service/chat_send_service.go:575) 遇到上游错误或发送失败，只要已有 token 就设置 `streamCompleted=true`，但没有设置 `partial`；清理路径因此保存为完整回答。刷新后用户看不到中断状态，后续模型也会把它当成普通历史。  
   应明确区分 `completed / partial / cancelled / failed`，不能用“已经发过内容”代表生成完成。

**其他确定的缺口与冗余**

7. **[P2] 用户消息已落库，但部分失败路径没有对应的回答终态。**  
   [session.go:87](E:/Codes/health-nexus/backend/internal/domain/chat/service/session.go:87) 的消息、标题、活跃时间更新没有统一事务；[chat_send_service.go:384](E:/Codes/health-nexus/backend/internal/domain/chat/service/chat_send_service.go:384) 保存用户消息后，还要经过历史读取、检索和引用发送才创建 assistant 占位。中间失败会留下孤立 user 消息，而后续历史按“消息数÷2”近似轮次。  
   建议先原子创建本轮 user 和 assistant 占位，再执行外部调用，所有出口都写入终态。

8. **[P2] 上一轮结束后的消息回拉，会覆盖下一轮的本地消息。**  
   [ChatConversation.vue:410](E:/Codes/health-nexus/frontend/src/chat/views/ChatConversation.vue:410) 在生成结束后异步拉取整页消息，此时发送按钮已经恢复。用户立即发送下一条后，旧快照可能通过 [chat.ts:155](E:/Codes/health-nexus/frontend/src/stores/chat.ts:155) 整体覆盖列表，导致刚发送的气泡消失。现有 epoch 只防止多个查询互相覆盖，没有保护查询与本地写入之间的竞争。  
   应按服务端消息 ID 合并，或按会话和请求版本拒绝过期快照。

9. **[P2] 历史分页只实现了后端，前端无法访问更早记录。**  
   [chat.ts:149](E:/Codes/health-nexus/frontend/src/stores/chat.ts:149) 始终无参数调用消息接口，后端默认只返回最近 50 条；页面没有继续加载 `before` 游标的入口。会话列表同样只加载第一页。长会话会表现为旧记录“消失”。  
   需要补齐消息向上分页和会话列表分页。

10. **[P2] 匿名消息“环”实际上没有容量上限。**  
    [session.go:195](E:/Codes/health-nexus/backend/internal/domain/chat/service/session.go:195) 每轮 `LRANGE 0 -1` 读取全部历史，再在应用内裁剪；写入只有追加和续期，没有 `LTRIM`。持续使用会不断增长，12 小时 TTL 因每轮刷新也不会限制总量。  
    应使用有界列表，原子完成追加、裁剪和续期，并只读取需要的尾部记录。

11. **[P2] 空会话过滤条件与数据库定义矛盾。**  
    [conversation_repo.go:72](E:/Codes/health-nexus/backend/internal/domain/chat/repository/conversation_repo.go:72) 用 `last_message_at IS NOT NULL` 隐藏空会话，但 [schema.sql:182](E:/Codes/health-nexus/backend/internal/di/schema.sql:182) 定义该字段为 `NOT NULL DEFAULT now()`。因此创建后遇到锁失败、LLM 不可用等情况，空会话仍会进入历史列表。  
    应让首条消息之前该字段为空，或按消息是否存在过滤。

12. **[P2] Rerank 前提前截断，扩大召回候选的设计失效。**  
    [search_service.go:154](E:/Codes/health-nexus/backend/internal/domain/wiki/service/search_service.go:154) 将候选先裁成 `topK`，随后才重排。即使前面查询了 `2×topK` 条，后半部分也永远没有机会被 Rerank 选中，既浪费查询，也削弱召回质量。  
    应先对候选集重排，再截取最终 `topK`。

**设计上建议收敛的方向**

现有 `Session + SessionStore` 统一认证和匿名路径是有价值的，但目前主要统一了方法签名，生命周期语义仍不一致。建议重点补齐以下四点：

- **引入明确的“本轮生成”实体。** 使用 `turn_id/request_id` 关联用户消息、assistant 消息、生成状态和幂等重试，解决孤立消息、重复提交和中断恢复。
- **让 SSE 返回权威结果。** 除 token 外，返回真实消息 ID、最终 `result_code` 和最终引用。目前前端自己猜结果码，再整页回拉；[按内容匹配真实消息 ID](E:/Codes/health-nexus/frontend/src/chat/views/ChatConversation.vue:263) 还可能把重复拒答的反馈提交到旧消息。
- **拆开提示与答案。** `safety_warning` 同时承担紧急提醒、拒答、超时提示和内容替换，前端又把它们混入答案正文，造成实时展示与持久化内容不一致。应明确各类事件的语义。
- **安全分类保留原因。** [LLM 审查失败分支](E:/Codes/health-nexus/backend/internal/domain/chat/service/chat_send_service.go:165) 统一进入普通拒答；即使模型判断的是自伤风险，也无法触发危机记录和热线流程。布尔值不足以表达这条链路，应返回结构化分类。

验证方面：前端 **156 项测试通过，类型检查通过**；后端聊天、RAG、安全过滤、LLM 相关测试通过。额外的 **5 个最小复现均暴露上述问题**，说明现有测试对跨层生命周期覆盖不足。未运行真实数据库、Redis 和模型服务的端到端验证；临时测试已移除，项目文件未改动。