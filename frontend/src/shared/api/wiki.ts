import { apiClient } from './client';
import type {
  ArticlePublic,
  ArticleDetail,
  ArticleStaff,
  ArticleChunk,
  ArticleCreateRequest,
  ArticleUpdateRequest,
  ArticleListParams,
  ArticleStaffListParams,
  ArticleReference,
  ReferenceApplyRequest,
  ReferenceListParams,
} from '../types/wiki';

// ===== 公共接口（匿名可访问） =====

/** 获取已发布文章列表（契约 §4.1） */
export function listArticles(params?: ArticleListParams) {
  return apiClient<{ items: ArticlePublic[]; total: number; page: number; page_size: number }>(
    '/wiki/articles',
    { params },
  );
}

export function listFeaturedArticles(departmentId?: number) {
  return apiClient<{ items: ArticlePublic[] }>('/wiki/articles/featured', {
    params: departmentId ? { department_id: departmentId } : undefined,
  });
}

/** 获取文章详情（契约 §4.2） */
export function getArticleDetail(articleId: number) {
  return apiClient<ArticleDetail>(`/wiki/articles/${articleId}`);
}

// ===== 医护端接口（JWT + RequireStaff） =====

/** 创建文章（契约 §4.3） */
export function createArticle(data: ArticleCreateRequest) {
  return apiClient<ArticleStaff>('/staff/wiki/articles', { method: 'POST', body: data });
}

/** 获取我的文章列表（契约 §4.4，含草稿/待审核等所有状态） */
export function listMyArticles(params?: ArticleStaffListParams) {
  return apiClient<{ items: ArticleStaff[]; total: number; page: number; page_size: number }>(
    '/staff/wiki/articles',
    { params },
  );
}

/** 获取单篇文章详情（含正文，用于编辑回填） */
export function getMyArticle(articleId: number) {
  return apiClient<ArticleStaff>(`/staff/wiki/articles/${articleId}`);
}

/** 更新文章（契约 §4.5） */
export function updateArticle(articleId: number, data: ArticleUpdateRequest) {
  return apiClient<ArticleStaff>(`/staff/wiki/articles/${articleId}`, { method: 'PUT', body: data });
}

/** 提交文章审核（契约 §4.6，draft → pending） */
export function submitArticle(articleId: number) {
  return apiClient<{ success: boolean }>(`/staff/wiki/articles/${articleId}/submit`, { method: 'POST' });
}

/** 删除文章（契约 §4.7，软删除） */
export function deleteArticle(articleId: number) {
  return apiClient<{ success: boolean }>(`/staff/wiki/articles/${articleId}`, { method: 'DELETE' });
}

/** 审核通过文章（契约 §4.8，pending → published） */
export function approveArticle(articleId: number, note?: string) {
  return apiClient<{ success: boolean }>(`/staff/wiki/articles/${articleId}/approve`, {
    method: 'POST',
    body: note ? { note } : undefined,
  });
}

/** 驳回文章（契约 §4.9，pending → draft，reason 必填） */
export function rejectArticle(articleId: number, reason: string) {
  return apiClient<{ success: boolean }>(`/staff/wiki/articles/${articleId}/reject`, {
    method: 'POST',
    body: { reason },
  });
}

/** 归档文章（published → archived） */
export function archiveArticle(articleId: number) {
  return apiClient<{ success: boolean }>(`/staff/wiki/articles/${articleId}/archive`, { method: 'POST' });
}

/** 取消归档（archived → published） */
export function unarchiveArticle(articleId: number) {
  return apiClient<{ success: boolean }>(`/staff/wiki/articles/${articleId}/unarchive`, { method: 'POST' });
}

export function setArticleFeatured(articleId: number, rank: number) {
  return apiClient<{ success: boolean }>(`/staff/wiki/articles/${articleId}/featured`, {
    method: 'POST',
    body: { rank },
  });
}

/** 列出文章生效切片（契约 §4.12，用于诊断 RAG 切片状态） */
export function listArticleChunks(articleId: number) {
  return apiClient<{ items: ArticleChunk[]; total: number }>(
    `/staff/wiki/articles/${articleId}/chunks`,
  );
}

/** 重新切片向量化（契约 §4.13，仅已发布文章可触发） */
export function revectorizeArticle(articleId: number) {
  return apiClient<{ success: boolean }>(`/staff/wiki/articles/${articleId}/revectorize`, {
    method: 'POST',
  });
}

// ===== 跨科室引用授权（契约 §5，6 个端点） =====

/** 发起引用申请（契约 §5.1，公开文章直接 approved，非公开文章返回 400） */
export function applyReference(data: ReferenceApplyRequest) {
  return apiClient<ArticleReference>('/staff/wiki/references', { method: 'POST', body: data });
}

/** 引用授权列表（契约 §5.2，分页） */
export function listReferences(params?: ReferenceListParams) {
  return apiClient<{ items: ArticleReference[]; total: number; page: number; page_size: number }>(
    '/staff/wiki/references',
    { params },
  );
}

/** 可引用的公开文章列表（其他科室的 allow_reference=true 文章） */
export function listReferenceableArticles(params?: { page?: number; page_size?: number }) {
  return apiClient<{ items: ArticlePublic[]; total: number; page: number; page_size: number }>(
    '/staff/wiki/references/articles',
    { params },
  );
}

/** 审核通过引用申请（契约 §5.3） */
export function approveReference(referenceId: number, note?: string) {
  return apiClient<{ success: boolean }>(`/staff/wiki/references/${referenceId}/approve`, {
    method: 'POST',
    body: note ? { note } : undefined,
  });
}

/** 驳回引用申请（契约 §5.4，reason 必填） */
export function rejectReference(referenceId: number, reason: string) {
  return apiClient<{ success: boolean }>(`/staff/wiki/references/${referenceId}/reject`, {
    method: 'POST',
    body: { reason },
  });
}

/** 撤销引用授权（契约 §5.5） */
export function revokeReference(referenceId: number) {
  return apiClient<{ success: boolean }>(`/staff/wiki/references/${referenceId}`, { method: 'DELETE' });
}
