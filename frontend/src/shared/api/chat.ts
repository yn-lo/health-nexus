import { apiClient } from './client';
import type {
  Conversation,
  ConversationListParams,
  ConversationUpdateRequest,
  Message,
  MessageListParams,
} from '../types/chat';

/**
 * chat 域 API — 对齐后端 chat 域 6 个患者端端点（契约 §3.1~3.6）。
 * 危机事件接口见 staffChat.ts（§3.7~3.8）。
 *
 * 注：后端无 POST 创建会话端点；新会话由 GET /api/chat/stream 在无 conversation_id 时隐式创建。
 *     后端无 POST 创建消息端点；用户消息由 SSE 流程持久化。
 */

/** 获取会话列表（契约 §3.2，分页） */
export function listConversations(params?: ConversationListParams) {
  return apiClient<{ items: Conversation[]; total: number; page: number; page_size: number }>(
    '/chat/conversations',
    { params },
  );
}

/** 获取会话详情（契约 §3.3） */
export function getConversation(conversationId: string) {
  return apiClient<Conversation>(`/chat/conversations/${conversationId}`);
}

/** 修改会话（标题/归档，至少一个字段，契约 §3.4） */
export function updateConversation(conversationId: string, data: ConversationUpdateRequest) {
  return apiClient<Conversation>(`/chat/conversations/${conversationId}`, { method: 'PATCH', body: data });
}

/** 删除会话（契约 §3.5） */
export function deleteConversation(conversationId: string) {
  return apiClient<{ success: boolean }>(`/chat/conversations/${conversationId}`, { method: 'DELETE' });
}

/** 获取会话消息列表（契约 §3.6，游标分页） */
export function listMessages(conversationId: string, params?: MessageListParams) {
  return apiClient<Message[]>(`/chat/conversations/${conversationId}/messages`, { params });
}

/** 提交消息反馈（点赞/点踩，成功返回 204 无响应体） */
export function submitMessageFeedback(messageId: string, feedback: 'up' | 'down') {
  return apiClient<void>(`/chat/messages/${messageId}/feedback`, { method: 'POST', body: { feedback } });
}
