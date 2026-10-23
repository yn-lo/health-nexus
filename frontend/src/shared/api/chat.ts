import {
  apiClient,
  clearTokens,
  getAccessToken,
  getDeviceId,
  getRefreshToken,
  tryRefreshToken,
} from './client';
import type {
  Conversation,
  ConversationListParams,
  ConversationUpdateRequest,
  Message,
  MessageListParams,
} from '../types/chat';
import type { MessageFeedback } from '../constants/feedback';
import type { Paginated } from '../types/base';

/**
 * chat 域 API — 对齐后端 chat 域 6 个患者端端点（契约 §3.1~3.6）。
 * 危机事件接口见 staffChat.ts（§3.7~3.8）。
 *
 * 注：后端无 POST 创建会话端点；新会话由 GET /api/chat/stream 在无 conversation_id 时隐式创建。
 *     后端无 POST 创建消息端点；用户消息由 SSE 流程持久化。
 */

/** 获取会话列表（契约 §3.2，分页） */
export function listConversations(params?: ConversationListParams) {
  return apiClient<Paginated<Conversation>>('/chat/conversations', { params });
}

export interface ChatStreamRequest {
  message: string;
  conversation_id: string;
  request_id: string;
  selected_dept_id?: number;
}

/**
 * 打开问答 SSE 流（POST）— 统一处理认证头、401 刷新与匿名降级。
 * 流式解析与业务事件留在 chat/composables/useSSEChat.ts。
 */
export async function openChatStream(body: ChatStreamRequest, signal: AbortSignal): Promise<Response> {
  let useAuth = !!getAccessToken();

  const doFetch = () => {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (useAuth) headers['Authorization'] = `Bearer ${getAccessToken()}`;
    else headers['X-Device-Id'] = getDeviceId();
    return fetch(useAuth ? '/api/chat/stream' : '/api/public/chat/stream', {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
      signal,
    });
  };

  let response = await doFetch();

  if (response.status === 401 && useAuth) {
    const attemptedRefresh = getRefreshToken();
    const refreshed = await tryRefreshToken();
    // 刷新成功，或 refresh token 已被其它标签页轮换（跨标签页并发刷新）→ 复用新 token 重试。
    if (refreshed || (getRefreshToken() !== null && getRefreshToken() !== attemptedRefresh)) {
      response = await doFetch();
    } else {
      // 会话彻底失效：清除本地登录态并降级为匿名继续本轮问答，避免硬跳登录打断访客。
      clearTokens();
      useAuth = false;
      response = await doFetch();
    }
  }

  return response;
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

/** 提交消息反馈（solved/partial/unsolved，成功返回 204 无响应体） */
export function submitMessageFeedback(messageId: string, feedback: MessageFeedback) {
  return apiClient<void>(`/chat/messages/${messageId}/feedback`, { method: 'POST', body: { feedback } });
}

/**
 * 删除匿名会话的服务端上下文（清除 Redis 瞬态环）。
 * 匿名无 JWT，以 X-Device-Id 标识设备；仅能清除本设备命名空间下的会话。
 */
export function deleteAnonConversation(conversationId: string) {
  return apiClient<{ success: boolean }>(`/public/chat/conversations/${conversationId}`, {
    method: 'DELETE',
    headers: { 'X-Device-Id': getDeviceId() },
  });
}
