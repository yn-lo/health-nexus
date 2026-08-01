/** 科室 — 对齐后端 DepartmentDTO（公开列表，不含 description/updated_at） */
export interface Department {
  id: number;
  name: string;
  is_public: boolean;
  parent_id: number | null;
  is_active: boolean;
  created_at: string;
}

/** 科室树节点 — 对齐后端 DepartmentTreeDTO（管理员视图，含 description/updated_at） */
export interface DepartmentTreeNode {
  id: number;
  name: string;
  parent_id: number | null;
  is_public: boolean;
  is_active: boolean;
  description: string;
  created_at: string;
  updated_at: string;
}

/** 创建科室请求 — 对齐后端 POST /api/staff/base/departments */
export interface DepartmentCreateRequest {
  name: string;
  parent_id?: number | null; // null/省略/0 = 根科室
  is_public?: boolean;       // 默认 false
  is_active?: boolean;       // 默认 true
  description?: string;      // 默认空串
}

/** 更新科室请求 — 对齐后端 PATCH /api/staff/base/departments/{id}
 *  全字段可选；parent_id 特殊：省略=不动，0=变根科室，N=移到 N 下 */
export interface DepartmentUpdateRequest {
  name?: string;
  description?: string;
  is_public?: boolean;
  is_active?: boolean;
  parent_id?: number;
}

/** 分页结果 — 对齐后端 pagination.NewResult */
export interface Paginated<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}
