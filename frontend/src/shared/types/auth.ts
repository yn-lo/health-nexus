import type { UserRole } from '../constants/roles';

/** 用户名密码登录请求（无 portal 字段 — 由调用方选择不同端点） */
export interface LoginRequest {
  username: string;
  password: string;
}

/** 注册请求 — 对齐后端 registerRequest（仅 username/password，DisallowUnknownFields） */
export interface RegisterRequest {
  username: string;
  password: string;
}

/** JWT Token 响应 — 对齐后端 StaffLoginResponse / PatientLoginResponse（含 user 字段） */
export interface TokenResponse {
  access: string;
  refresh: string;
  user: TokenUser;
}

/** 登录响应中的用户信息 */
export interface TokenUser {
  id: number;
  username: string;
  role: UserRole;
  phone: string;
  date_of_birth: string | null;
  gender: string;
  emergency_contact: string;
  emergency_phone: string;
  dept_id: number;
}

/** 通用成功响应 — 对齐后端 successResponse { success: true } */
export interface SuccessResponse {
  success: boolean
}

/** POST /api/auth/password-reset/request — 请求密码重置 */
export interface PasswordResetRequestRequest {
  username: string
}

/** POST /api/auth/password-reset/confirm — 确认密码重置 */
export interface PasswordResetConfirmRequest {
  token: string
  new_password: string
}

/** POST /api/auth/change-password — 已登录用户修改密码 */
export interface ChangePasswordRequest {
  old_password: string
  new_password: string
}

/** PATCH /api/auth/profile — 更新个人资料 */
export interface UpdateProfileRequest {
  phone: string
  date_of_birth: string | null
  gender: string
  emergency_contact: string
  emergency_phone: string
}

/** 管理员视角的账户信息 — 对齐后端 service.AccountDTO */
export interface StaffAccount {
  id: number
  username: string
  role: UserRole
  phone: string
  date_of_birth: string | null
  gender: string
  emergency_contact: string
  emergency_phone: string
  primary_dept_id: number
  primary_dept_name: string
  is_active: boolean
  is_deleted: boolean
  created_at: string
}

/** POST /api/staff/auth/accounts — 管理员创建账户请求体 */
export interface StaffAccountCreateRequest {
  username: string
  password: string
  role: UserRole
  dept_id: number
}

/** POST /api/staff/auth/accounts/{id}/reset-password — 管理员重置用户密码请求体 */
export interface ResetPasswordRequest {
  new_password: string
}
