 
# Health-Nexus「术后需要注意什么」被过度追问 —— 问题定位与修复方案

## 0. 背景/环境
- 主机、路径、容器、部署命令

## 1. 现象
## 2. 根因（一句话）
## 3. 证据
### 3.1 运行时链路
### 3.2 日志
### 3.3 数据库
### 3.4 代码定位
## 4. 修复方案（分步，含签名/片段）
### 必改
### 可选
## 5. 验证与部署
## 6. 回归要点

Let me write it. I'll keep code snippets accurate.

Make sure to give exact file paths relative to repo root `/www/dk_project/Health-Nexus`.

Let me write.
# Health-Nexus「术后需要注意什么」被 AI 追问 —— 问题定位与修复交接单

> 环境：新加坡-甲骨文-A1（140.245.123.101，ubuntu）· 仓库 `/www/dk_project/Health-Nexus` · compose 项目 `health-nexus` · 容器 `hn-server`/`hn-worker`/`hn-postgres`/`hn-redis`。以下路径均相对仓库根。

## 1. 现象
admin（user_id=1）登录 → 选中「胸外科」→ 提问「术后需要注意什么」。AI 回复 **“为了给您更准确的宣教信息，还需要您补充一点情况：请问您做的是哪种手术？术后多长时间了？”**，且**不显示任何参考资料**。用户预期：知识库没有相关内容时应直接返回预设兜底话术。

## 2. 根因（一句话）
回复「追问」的不是作答大模型，而是**每轮必经的前置「统一理解与审查」模块（Assessor）**：它判定 `context_sufficient=false` → `Action=clarify`，该分支**先澄清、不检索、不生成**并直接短路返回。因此既没走检索，也就永远不会触发「无参考文章→预设话术」那条分支。**且 Assessor 的提示词完全不含当前科室**，导致模型看到孤立的「术后…」只能追问术式。

## 3. 证据

**3.1 运行时链路**（`backend/internal/domain/chat/service/chat_send_service.go` → `Stream`）
顺序为：规则快筛 → **Assessor（每轮无条件 AI 调用，`dispatchByAssessment` 之前）** → 按 Action 分流。
- `ActionClarify` 分支（约 L343-351）注释原文即「关键信息不足：先澄清，不检索不生成」，走 `handleFixedReply(...)`，且最终 `writeTurnResult(..., nil)` → **不产生 references 事件**，所以前面不列参考资料。
- 期望的「无参考→预设」是 `prepareRAGContext` 的 **0 命中分支**（约 L1223-1231，`NoKnowledgeMessage()`/`OutOfDomainMessage()`），**只在 `Action=retrieve` 时才会执行**。本问题根本没进到那里。

**3.2 日志**（`docker logs hn-server`）
```
2026-10-13T23:25:08 "chat: assess done" action=clarify context_sufficient=false out_of_domain=false user_id=1  (trace 3e023283…)
2026-10-13T23:25:08 "chat: request completed" result_code=REJECTED
```
该 trace **没有任何 `wiki: RAG search` / `chat: RAG search completed` 日志**（对比同时段 23:24:18 的 retrieve 轮有这些行）→ 证明检索阶段未执行。
同一追问在 2026-09-24「术后恢复期活动和复查要注意什么」也复现，属稳定行为。

**3.3 数据库**
- 会话 `9d954aa0-40a6-444f-a682-ab8e9bec9fc4`：`patient_id=1`、**`locked_dept_id=1`（胸外科）**、标题「术后需要注意什么」。
- assistant 消息 `result_code=REJECTED`，内容即上述追问。
- **dept=1 胸外科本身有已发布文章 id=1《胸外科术后健康宣教》（3 个切片，内容正是术后疼痛管理/呼吸训练）** → 若走检索，极可能命中。
- `rag_configs`：`similarity_threshold=0.75`（**DB 覆盖了默认 0.5**）、`top_k=5`、`rerank_enabled=t`。
- `prompt_templates`：**空表** → 生成用系统提示词恒为代码常量 `constants.DefaultSystemPrompt`。

**3.4 关键代码定位**
- Assessor 提示词：`backend/internal/adapter/llm_assessor.go`（`assessSystemPrompt` L28-62、`buildAssessUserContent` L128-151）——**无任何科室信息**。
- 科室名其实已解析出来但被丢弃：`chat_send_service.go` `resolveDeptAndConversation`（L739-751）返回 `rag.Department{ID,Name}`，而 `buildSession`（L641-661）只保留 `DeptID`。
- `Session` 结构（`session.go` L71-75）只有 `DeptID`，无名称。
- 前端**仅首条消息**发送 `selected_dept_id`（`frontend/src/chat/composables/useSSEChat.ts` L115-116：`if (!conversationId.value && options.selectedDeptId != null)`）→ 续聊不重发，因此**不能**简单依赖 `dept.Name`（续聊时 `ResolveForPatient(nil)` 取到的是患者主科室，可能≠锁定科室）。

## 4. 修复方案

### 4.1 必改：把「当前科室名」注入 Assessor（核心）

**(1) `Session` 增加字段** — `backend/internal/domain/chat/service/session.go`
```go
type Session struct {
	SID      string
	DeptID   *int64
	DeptName string // 科室名（提示词上下文）；空表示不限科室/未知
	store    SessionStore
}
```

**(2) 解析器增加按 id 取名** — `backend/internal/shared/rag/search.go` 的 `DepartmentResolver`
```go
// ResolveByID 按科室 id 解析（用于会话已锁定、续聊未带 selected_dept_id 时回填名称）。
// 未找到返回零值 Department（ID=0），不返回错误。
ResolveByID(ctx context.Context, deptID int64) (Department, error)
```
实现于 `backend/internal/adapter/base_adapter.go`（`BaseDepartmentResolver`），复用已有的 `r.repo.GetByID(ctx, deptID)`（该方法已被 `BaseDepartmentLookup.GetByID` 使用）。

**(3) `buildSession` 回填名称** — `chat_send_service.go` L641-661，认证分支改为：
```go
dept, conv, err := s.resolveDeptAndConversation(ctx, in)
if err != nil { return nil, err }
effDeptID := deptIDPtr(in.SelectedDeptID, conv)
deptName := ""
if effDeptID != nil {
	deptName = dept.Name
	if dept.ID != *effDeptID { // 续聊锁定科室：本次解析可能是患者主科室，按锁定 id 回填
		if d, rerr := s.dept.ResolveByID(ctx, *effDeptID); rerr == nil {
			deptName = d.Name
		}
	}
}
store := newDBSessionStore(s.conv, s.msg, s.crisis, s.crisisNotifier, s.tx, conv)
return &Session{SID: conv.ID.String(), DeptID: effDeptID, DeptName: deptName, store: store}, nil
```
匿名分支：`resolveAnonDept` 改为返回 `(*int64, string)`，并把 `id` 与 `name` 一起持久化到 Redis（建议存 JSON，或 `id|name`；对应的 `loadAnonDept`/`storeAnonDept` 一并改签名）。`sessionByID`（L581-598）可选同步 `DeptName`（该路径仅幂等回放，不参与审查/生成）。

**(4) Assessor 接口与实现带科室名**
- `backend/internal/shared/rag/assessment.go` L186：
```go
AssessAndRewrite(ctx context.Context, message, deptName string, history []AssessTurn) (Assessment, error)
```
- `backend/internal/adapter/llm_assessor.go`：
  - 同步 `AssessAndRewrite` 签名；`buildAssessUserContent` 增加 `deptName` 参数，在「对话历史」前插入一行：
    `当前科室：{deptName}`；为空时写 `当前科室：未指定（不限，全部科室）`。
  - `assessSystemPrompt` 增补一条判定规则（保证科室已限定时不再机械追问）：
    > 若已知「当前科室」，请结合科室语境理解问题：科室已限定的常规宣教问题（如“术后注意什么”“饮食注意什么”“出院注意什么”）视为信息充分，直接给出检索改写并置 `context_sufficient=true`，**不要**因未说明具体病种/术式而追问。仍需追问的场景仅限信息缺失会显著影响本次回答的（如个体化用药、症状鉴别）。
  - `chat: assess done` 日志增加 `dept` 字段（便于排查）。
- 调用点 `chat_send_service.go`：`assess()`（L402-409）需拿到科室名——它由 `loadSnapshotAndAssess(ctx, in, sess)`（L268-280）调用，建议把 `sess.DeptName` 传下去，L408 改为：
```go
return s.assessor.AssessAndRewrite(ctx, in.Message, deptName, toAssessTurns(history))
```

**(5) 同步修改所有实现/调用方（否则编译失败）**
- `backend/internal/adapter/llm_assessor_test.go` L72、L99 → 加 `""` 参数
- `backend/internal/domain/chat/service/chat_send_service_integration_test.go` L75 `mockAssessor` → 新签名；`mockDeptResolver`（L36/L41 附近）→ 补 `ResolveByID`
- `backend/tests/eval/run_test.go` L143 → 加 `""`（评测无科室概念）

### 4.2 可选（同源问题，建议一并做）
- **生成侧也带科室**：`buildSystemPrompt`（`chat_send_service.go` L1560-1569）在返回提示词后追加一行 `当前科室：{DeptName}`（注意 `prompt_templates` 为空，改的是代码常量/拼接逻辑）。作用范围更大，需回归评估文案影响。
- **澄清与拒答区分结果码**：`ActionClarify` 目前记 `ResultRejected`，与真拒答同码，污染统计；可新增 `CLARIFIED`。
- **复核 RAG 阈值**：DB 现值 0.75（默认 0.5）。恢复检索后若仍“有知识答不出”，优先调低此项。

> 注意：不要把「澄清」整体删掉——对「我现在能不能加药」这类个体化问题必须保留澄清/受限逻辑（`assessment.go` 的 `Action()` 优先级）。本方案只是让**科室已限定的常规宣教问题**不要被误判为信息不足。

## 5. 验证与部署
1. 单测：`cd backend && go test -race -count=1 ./internal/...`（Makefile 目标 `make test`）；建议补一条用例：`DeptName="胸外科"` 时「术后需要注意什么」应 `ActionRetrieve`。
2. 构建部署（在 `/www/dk_project/Health-Nexus`）：
```
sudo docker compose build server worker
sudo docker compose up -d server worker
```
3. 复现验收：admin → 新建会话选「胸外科」→ 发「术后需要注意什么」。**期望**：日志出现 `action=retrieve` + `wiki: RAG search` + `chunks≥1`，回答含参考资料，**不再出现 `action=clarify`**。

## 6. 回归要点
- 续聊（**不发** `selected_dept_id`）时科室名仍正确（靠 `ResolveByID` 回填）。
- 匿名会话（首条带 `selected_dept_id`、续聊靠 Redis）科室名可正确恢复。
- 「全部科室」/未选科室：提示词应为“未指定”，不得瞎编科室名。
- 医疗安全回归：用药调整/个体化诊断仍走 `restricted`，自伤/急症仍走 crisis/emergency；评测门禁 `make verify`。

 