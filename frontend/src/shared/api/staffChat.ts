/**
 * 医护端 chat API — 对齐后端 staff/chat 端点
 * api-contracts.md §3.7 / §3.8
 */
import { apiClient } from './client';
import type {
  CrisisEventItem,
  CrisisEventListParams,
  CrisisEventHandleRequest,
} from '../types/staffChat';
import type { Paginated } from '../types/base';

/** 获取危机事件列表（分页） */
export function listCrisisEvents(params?: CrisisEventListParams) {
  return apiClient<Paginated<CrisisEventItem>>('/staff/chat/crisis-events', { params });
}

/** 处理危机事件（标记已处理，可附备注） — eventId 对齐后端 int64 ID */
export function handleCrisisEvent(eventId: number, data?: CrisisEventHandleRequest) {
  return apiClient<{ success: boolean }>(
    `/staff/chat/crisis-events/${eventId}/handle`,
    { method: 'POST', body: data ?? {} },
  );
}
