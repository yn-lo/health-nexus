/** 文章状态 — 对齐后端 constants.ArticleStatus*（draft|pending|published|archived|deleted） */
export type ArticleStatus = 'draft' | 'pending' | 'published' | 'archived' | 'deleted';

/** 文章公共信息（列表项，对齐后端 ArticleListItemDTO） */
export interface ArticlePublic {
  id: number;
  title: string;
  summary: string;
  cover_url: string;
  department_id: number | null;
  department_name: string;
  view_count: number;
  version: number;
  allow_reference: boolean;
  featured_rank: number;
  published_at: string | null;
  created_at: string;
}

/** 文章详情（对齐后端 ArticleDetailDTO） */
export interface ArticleDetail {
  id: number;
  title: string;
  content: string;
  summary: string;
  cover_url: string;
  department_id: number | null;
  department_name: string;
  view_count: number;
  version: number;
  allow_reference: boolean;
  author_id: number;
  author_name: string;
  published_at: string | null;
  created_at: string;
}

/** 医护端文章视图（对齐后端 ArticleStaffDTO，含所有状态） */
export interface ArticleStaff {
  id: number;
  title: string;
  content: string;
  summary: string;
  cover_url: string;
  status: ArticleStatus;
  version: number;
  department_id: number | null;
  department_name: string;
  author_id: number;
  author_name: string;
  reviewer_id: number | null;
  review_comment: string | null;
  view_count: number;
  allow_reference: boolean;
  featured_rank: number;
  published_at: string | null;
  created_at: string;
  updated_at: string;
}

/** 创建文章请求（对齐契约 §4.3） */
export interface ArticleCreateRequest {
  title: string;
  content: string;
  summary?: string;
  cover_url?: string;
  department_id: number;
  allow_reference?: boolean;
}

/** 更新文章请求（对齐契约 §4.5） */
export interface ArticleUpdateRequest {
  title?: string;
  content?: string;
  summary?: string;
  cover_url?: string;
  allow_reference?: boolean;
  /** 编辑时加载到的版本号；传入启用乐观锁，并发编辑冲突后端返回 409 */
  version?: number;
}

/** 已发布文章列表查询参数（对齐契约 §4.1） */
export interface ArticleListParams {
  department_id?: number;
  search?: string;
  page?: number;
  page_size?: number;
}

/** 医护端文章列表查询参数（对齐契约 §4.4） */
export interface ArticleStaffListParams {
  status?: ArticleStatus;
  department_id?: number;
  page?: number;
  page_size?: number;
}

// ===== 跨科室引用授权（契约 §5） =====

/** 引用授权状态 */
export type ReferenceStatus = 'pending' | 'approved' | 'rejected' | 'revoked';

/** 文章切片（对齐后端 ArticleChunkDTO，契约 §4.12） */
export interface ArticleChunk {
  id: number;
  chunk_index: number;
  content: string;
  content_hash: string;
  version: number;
  created_at: string;
}

/** 引用授权记录（对齐后端 ReferenceDTO） */
export interface ArticleReference {
  id: number;
  article_id: number;
  article_title: string;
  source_dept_id: number;
  source_dept_name: string;
  target_dept_id: number;
  target_dept_name: string;
  status: ReferenceStatus;
  applicant_id: number;
  applicant_name: string;
  reviewer_id: number | null;
  reviewed_at: string | null;
  review_note: string | null;
  source_article_status: string; // 源文章当前状态，非 published 时前端显示变动提示
  created_at: string;
}

/** 发起引用申请请求 */
export interface ReferenceApplyRequest {
  article_id: number;
  target_dept_id: number;
}

/** 引用列表查询参数 */
export interface ReferenceListParams {
  status?: ReferenceStatus;
  direction?: 'outgoing' | 'incoming';
  page?: number;
  page_size?: number;
}
