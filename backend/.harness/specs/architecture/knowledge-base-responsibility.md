---
last_updated: 2026-10-14
status: active
owner: backend-team
---

# 知识库可见性与责任归属

本文澄清「科室知识库」的可见性规则与责任归属，避免将「公共科室」误当作全局可见区。
涉及代码：`wiki/repository/chunk_repo.go`（`deptVisibilitySQL`）、`wiki/service/reference_service.go`、
`chat/service/chat_send_service.go`、`adapter/base_adapter.go`。

## 1. 基本术语

| 术语 | 含义 |
|------|------|
| 源科室 | 文章 `articles.department_id` 所属科室（内容作者） |
| 目标科室 | 发起引用、把他人文章纳入自己知识库的科室（内容使用方） |
| `allow_reference` | 源科室是否**开放引用**该文章（`articles.allow_reference`） |
| 公共科室 | 面向全院/全部科室提供通用健康宣教内容的科室；**不是全局可见区** |

## 2. 可见性规则（唯一真源：`deptVisibilitySQL`）

用户在某个科室下检索时，可见的文章集合为：

```
可见 = 本科室文章 ∪ 本科室已 approved 的引用文章
```

- 未选择任何科室（全库检索）时不做科室过滤（`1=1`）。
- **选定具体科室后，不会自动并入公共科室的文章**。公共科室的文章只有被该科室
  通过引用审批流程纳入（`article_references` 状态为 `approved`）后才可见。
- 因此「公共科室」是**内容供给方**，而非全局可见区。

## 3. 责任归属

- **科室管理员对其本科室知识库中的宣教内容负责。**
- 公共科室可以向其他科室供给通用宣教内容（`allow_reference = true`）。
- 独立科室**自主决定是否纳入（引用）**公共科室分享的文章：
  - 发起引用申请（`pending`）→ 经审批（`approved`）→ 该科室可见并**对该内容负责**。
  - 未纳入（无 approved 引用）→ 检索不可见，不承担任何责任。
- 参考资料之间若表述冲突，生成侧**必须并列说明差异**，不得静默择一
  （`DefaultSystemPrompt` 第 9 条）。
- 匿名用户看到的内容仅作一般性宣教，不构成科室级承诺（匿名不落库、不转交医护）。

## 4. 引用流程

1. 源科室文章 `allow_reference = true`（未开放引用的文章不可被引用，返回 400 `WIKI_REF_NOT_ALLOWED`）。
2. 目标科室成员发起申请：`POST /api/staff/wiki/references` → 创建 `pending` 引用。
   - 纳入**必须经过审批**：`Apply` 只产生 `pending`，不会直接 `approved`。
3. 审批：`POST /api/staff/wiki/references/{id}/approve`（`pending → approved`）。
   - 审批权限（`assertCanReviewReference`）：**源科室 `DEPT_ADMIN` 或 `SUPER_ADMIN`**；
     申请人不得审核自己的申请（`WIKI_REF_SELF_REVIEW`，单超管场景有豁免）。
4. 通过后目标科室检索即可见；`DELETE /api/staff/wiki/references/{id}` 撤销（`approved → revoked`）。
5. 源文章被**删除/归档**时，系统自动撤销其全部 approved 引用
   （`ArticleService.revokeReferencesAfterArticleChange`）。

> **待办（未来实现）**：源文章**内容变更（Update/重新发布）**当前**不会**撤销引用，
> 引用科室会在未重新确认的情况下继续使用新内容。计划：在源文章产生新版本时
> **提醒引用科室管理员**（可选：要求重新纳入）。本轮不实现。

## 5. 问答（chat）责任科室

为避免「统一理解与审查」在无科室语境时对「术后注意什么」这类常规宣教问题
机械追问，问答路径必须始终落到一个**具体责任科室**：

- Chat 页面（`ChatHome` / `ChatConversation`）**取消「全部科室」选项**，只能聚焦具体科室。
- 默认责任科室由科室列表的 `is_default` 标记决定，前端据此预选。
- 后端兜底：会话未指定科室（`selected_dept_id` 为 `nil/0`，或历史会话无锁定科室）时，
  落到 `chat.default_dept_id`；未配置（`0`）时取第一个 `is_public` 且启用的科室（生产即「公共科室」）。
  - 配置项：`chat.default_dept_id`（环境变量 `HEALTH_NEXUS_CHAT_DEFAULT_DEPT_ID`）。
  - 实现：`adapter.BaseDepartmentResolver.ResolveDefault`（与 `DepartmentService.effectiveDefaultDeptID` 口径一致）。
- 「全部科室」（全库检索）仅保留在**知识库浏览**路径（文章列表/筛选），不进入问答作答。
- 责任科室已明确且审查意图为**患者教育**（`patient_education`）时，即使审查判 `clarify` 也在代码层
  降级为检索（落实审查规则 7，消除 LLM 判定非确定性导致的"有知识却反复澄清/暂无"）；
  检索 0 命中时由"暂无相关内容"兜底。安全动作（crisis/emergency/restricted/reject）优先级更高，不受影响。

## 6. 引用编号单位

答案中的 `[资料N]` 的 N 以**文章**为单位：同篇文章的多个命中切片在后端归并为一条，
编号 = 文章首次出现顺序，与前端"参考来源"列表（按 `article_id` 去重）逐项对应。
实现：检索后 `groupChunksByArticle` 归并，生成侧 `chunkContentsForLLM`、审核侧证据、
落库 `referenced_chunks`、SSE `references` 事件均复用该归并结果，避免"列表 1 条、正文却引用 [资料3]"。

**患者端"参考来源"只展示答案实际引用的文章**（由正文 `[资料N]` 反解得到，按首次引用顺序去重，
角标序号 = 该文章在引用列表中的位置）；未被正文引用的候选文章不展示——避免暗示答案依据了未使用的内容。
`messages.referenced_chunks` 仍保留完整候选集供审计；答案零引用时列表为空并记
`chat: answer cites no reference despite retrieved candidates` 告警（引用合规率可度量）。
归一逻辑：`frontend/src/shared/utils/citations.ts`。

## 7. 相关代码位置

- 可见性 SQL：`internal/domain/wiki/repository/chunk_repo.go` `deptVisibilitySQL`
- 引用申请/审批/撤销：`internal/domain/wiki/service/reference_service.go`
- 文章变更撤销引用：`internal/domain/wiki/service/article_service.go`
- 问答会话科室解析：`internal/domain/chat/service/chat_send_service.go`（`buildSession` / `resolveAnonDept`）
- 默认责任科室：`internal/adapter/base_adapter.go` `ResolveDefault`
- 默认科室标记：`internal/domain/base/service/department_service.go` `effectiveDefaultDeptID` / `toDepartmentDTO`
