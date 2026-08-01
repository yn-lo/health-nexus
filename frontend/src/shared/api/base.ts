import { apiClient } from './client';
import type {
  Department,
  DepartmentTreeNode,
  DepartmentCreateRequest,
  DepartmentUpdateRequest,
} from '../types/base';
import type { SuccessResponse } from '../types/auth';

// ===== 公开（无需认证） =====

/** 获取公共科室列表（GET /api/public/departments，无需认证） */
export function listPublicDepartments() {
  return apiClient<Department[]>('/public/departments');
}

// ===== 只读（医护通用） =====

/** 获取当前用户可见的科室列表（GET /api/base/departments） */
export function listDepartments() {
  return apiClient<Department[]>('/base/departments');
}

// ===== 管理员 CRUD（GET/POST/PATCH/DELETE /api/staff/base/departments） =====

/** 获取科室树（扁平数组，前端按 parent_id 组装）
 *  SUPER_ADMIN 返回全树；DEPT_ADMIN 仅返回主科室子树 */
export function listDepartmentTree() {
  return apiClient<DepartmentTreeNode[]>('/staff/base/departments');
}

/** 创建科室 */
export function createDepartment(data: DepartmentCreateRequest) {
  return apiClient<DepartmentTreeNode>('/staff/base/departments', { method: 'POST', body: data });
}

/** 更新科室（PATCH，部分字段） */
export function updateDepartment(id: number, data: DepartmentUpdateRequest) {
  return apiClient<DepartmentTreeNode>(`/staff/base/departments/${id}`, { method: 'PATCH', body: data });
}

/** 删除科室（需无子科室、无关联用户） */
export function deleteDepartment(id: number) {
  return apiClient<SuccessResponse>(`/staff/base/departments/${id}`, { method: 'DELETE' });
}
