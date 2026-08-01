/**
 * 医护端 chat 模块类型 — 对齐后端 staff/chat 端点
 * 对应 api-contracts.md §3.7（GET /staff/chat/crisis-events）和 §3.8（POST /staff/chat/crisis-events/{id}/handle）
 */

/** 危机事件级别 — 对齐后端 CrisisEvent.Level（high|medium|low） */
export type CrisisLevel = 'high' | 'medium' | 'low'

/** 危机事件列表项 — 对齐后端 CrisisEventResponse（ID/PatientID/HandlerID 均为 int64） */
export interface CrisisEventItem {
  id: number
  patient_id: number
  patient_name: string
  conversation_id: string
  /** 触发内容 — 后端字段名 triggered_content */
  triggered_content: string
  /** 匹配关键词（后端返回 string[]，前端 join 展示） */
  matched_keywords: string[]
  level: CrisisLevel
  /** 是否已处理 — 后端字段名 handled */
  handled: boolean
  /** 处理人 ID — 后端 *string，未处理时为 null */
  handler_id: string | null
  /** 处理时间 — 后端 *string */
  handled_at: string | null
  /** 处理备注 — 后端 *string */
  handle_note: string | null
  created_at: string
}

/** 危机事件列表查询参数 */
export interface CrisisEventListParams {
  /** 按处理状态过滤：true=仅已处理，false=仅未处理，undefined=全部 */
  handled?: boolean
  /** 按级别过滤 */
  level?: CrisisLevel
  page?: number
  page_size?: number
}

/** 危机事件处理请求 */
export interface CrisisEventHandleRequest {
  note?: string
}

