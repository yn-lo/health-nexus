/**
 * chat 域类型 — 对齐后端 ConversationResponse / MessageResponse
 * 见 backend/internal/domain/chat/service/conversation_service.go
 */

/** 会话 — 对齐后端 ConversationResponse（契约 §3.2~3.5） */
export interface Conversation {
  id: string;
  title: string;
  /** 锁定科室 ID（首条消息后锁定，null 表示未锁定） */
  locked_dept_id: number | null;
  archived: boolean;
  last_message_at: string | null;
  created_at: string;
}

/** 修改会话请求（至少一个字段，对应 PATCH /chat/conversations/{id}） */
export interface ConversationUpdateRequest {
  title?: string;
  archived?: boolean;
}

/** 会话列表查询参数（契约 §3.2） */
export interface ConversationListParams {
  /** true 时包含已归档会话，默认 false */
  archived?: boolean;
  page?: number;
  page_size?: number;
}

/** 引用切片 — 对齐后端 Reference（契约 §3.6 references 字段） */
export interface Reference {
  chunk_id: string;
  article_id: number;
  article_title: string;
  content: string;
  score: number;
}

/** 消息 — 对齐后端 MessageResponse（契约 §3.6） */
export interface Message {
  /** UUID 字符串；本地乐观插入时使用临时字符串 ID（如 Date.now().toString()） */
  id: string;
  conversation_id: string;
  /** 角色：user=患者提问，assistant=AI 回答，system=系统消息 */
  role: 'user' | 'assistant' | 'system';
  content: string;
  /** 处理结果码 — ANSWERED/PARTIAL/REJECTED/INTERCEPTED/CRISIS/RATE_LIMITED，user 消息为 null */
  result_code: string | null;
  /** AI 回答引用的知识切片；user 消息为空数组 */
  references: Reference[];
  /** 患者反馈 — up=点赞 / down=点踩 / null=未反馈 */
  feedback?: 'up' | 'down' | null;
  created_at: string;
}

/** 消息列表查询参数（契约 §3.6，游标分页） */
export interface MessageListParams {
  /** 单页大小，默认 50，最大 200 */
  limit?: number;
  /** 游标：返回该 UUID 消息之前的记录 */
  before?: string;
}

/** SSE 流式事件 — 对齐后端 sseWriter.Write 的事件名（stream_handler.go） */
export type SSEEvent =
  | { type: 'conversation'; data: { conversation_id: string } }
  | { type: 'token'; data: string }
  | { type: 'references'; data: Reference[] }
  | { type: 'safety_warning'; data: string; mode?: 'replace' | 'append' }
  | { type: 'crisis'; data: { answer: string } }
  | { type: 'error'; data: { message: string } }
  | { type: 'done'; data: '[DONE]' };
