/** 文章状态 — 对齐后端 constants.ArticleStatus*（draft|pending|published|archived|deleted） */
export type ArticleStatus = 'draft' | 'pending' | 'published' | 'archived' | 'deleted';

/**
 * 内容风险等级 — 对齐后端 entity.ContentRiskNormal / ContentRiskHigh
 * high（用药、检查准备、高风险护理）复审周期更短，且逾期后退出检索
 */
export type ContentRisk = 'normal' | 'high';

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
  /** 编辑稿：作者最新提交的正文（待重新审核期间可能是未审核内容，仅医护端可见） */
  content: string;
  /** 最近审核通过的正文快照（患者端所见版本）；与 content 不同表示有待审核的新版本 */
  published_content: string;
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
  /** 知识来源（如指南名称）— 用于复审追溯与检索可见性 */
  source: string;
  /** 适用人群（如"高血压患者""孕产妇"）— 用于检索可见性 */
  applicable_population: string;
  /** 有效期至（RFC3339；null 表示未设置/长期有效）— 用于复审提醒与检索可见性 */
  valid_until: string | null;
  /** 内容风险等级：normal=普通宣教，high=用药/检查准备/高风险护理（高风险逾期后退出检索） */
  content_risk: ContentRisk;
  /** 是否已超过复审周期（后端按风险等级计算） */
  review_overdue: boolean;
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
  /** 知识来源（省略该字段=不更新） */
  source?: string;
  /** 适用人群（省略该字段=不更新） */
  applicable_population?: string;
  /** 有效期至（RFC3339；传空字符串 "" 表示清空有效期，非法格式后端返回 422 WIKI_VALID_UNTIL_INVALID） */
  valid_until?: string;
  /** 内容风险等级（省略该字段=不更新；非法值后端返回 422 WIKI_CONTENT_RISK_INVALID） */
  content_risk?: ContentRisk;
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
