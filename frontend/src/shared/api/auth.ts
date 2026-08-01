import { apiClient, setTokens, clearTokens, getRefreshToken } from './client';
import type {
  LoginRequest,
  RegisterRequest,
  TokenResponse,
  SuccessResponse,
  PasswordResetRequestRequest,
  PasswordResetConfirmRequest,
  ChangePasswordRequest,
  UpdateProfileRequest,
  TokenUser,
  StaffAccount,
  StaffAccountCreateRequest,
  ResetPasswordRequest,
} from '../types/auth';
import type { Paginated } from '../types/base';

/** 用户登出 — 后端要求 body 传 refresh token 才能加黑名单 */
export function logout() {
  const refresh = getRefreshToken();
  // ponytail: 即使本地无 refresh token 也发请求清服务端会话；后端会校验 refresh 非空，折中
  return apiClient<{ success: boolean }>('/auth/logout', {
    method: 'POST',
    body: refresh ? { refresh } : {},
  }).finally(() => {
    clearTokens();
  });
}

/** 统一登录并存储 token（按 role 自动路由，无需选择 portal） */
export async function loginAndStore(data: LoginRequest) {
  const tokens = await apiClient<TokenResponse>('/auth/login', { method: 'POST', body: data });
  setTokens(tokens.access, tokens.refresh);
  return tokens;
}

/** 注册并存储 token（便捷方法） */
export async function registerAndStore(data: RegisterRequest) {
  const tokens = await apiClient<TokenResponse>('/auth/register', { method: 'POST', body: data });
  setTokens(tokens.access, tokens.refresh);
  return tokens;
}

/** 请求密码重置 — 始终返回成功（安全设计：不泄露用户是否存在） */
export function requestPasswordReset(username: string) {
  return apiClient<SuccessResponse>('/auth/password-reset/request', {
    method: 'POST',
    body: { username } satisfies PasswordResetRequestRequest,
  });
}

/** 确认密码重置 — 校验 token 并更新密码（token 一次性使用） */
export function confirmPasswordReset(token: string, newPassword: string) {
  return apiClient<SuccessResponse>('/auth/password-reset/confirm', {
    method: 'POST',
    body: { token, new_password: newPassword } satisfies PasswordResetConfirmRequest,
  });
}

/** 已登录用户修改密码 — userID 由 JWT 注入，仅传新旧密码 */
export function changePassword(oldPassword: string, newPassword: string) {
  return apiClient<SuccessResponse>('/auth/change-password', {
    method: 'POST',
    body: { old_password: oldPassword, new_password: newPassword } satisfies ChangePasswordRequest,
  });
}

/** 读取已登录用户个人资料 */
export function getProfile() {
  return apiClient<TokenUser>('/auth/profile', { method: 'GET' });
}

/** 更新已登录用户个人资料 */
export function updateProfile(data: UpdateProfileRequest) {
  return apiClient<TokenUser>('/auth/profile', {
    method: 'PATCH',
    body: data satisfies UpdateProfileRequest,
  });
}

// ===== 管理员账户管理（GET/POST /api/staff/auth/accounts，POST .../lock|unlock） =====

/** 分页查询全部账户 */
export function listStaffAccounts(params?: { page?: number; page_size?: number }) {
  return apiClient<Paginated<StaffAccount>>('/staff/auth/accounts', { params });
}

/** 创建账户（角色权限收口在后端 service 层） */
export function createStaffAccount(data: StaffAccountCreateRequest) {
  return apiClient<StaffAccount>('/staff/auth/accounts', { method: 'POST', body: data });
}

/** 锁定账户（后端禁止锁定自己，返回 409 AUTH_SELF_LOCK） */
export function lockStaffAccount(id: number) {
  return apiClient<SuccessResponse>(`/staff/auth/accounts/${id}/lock`, { method: 'POST' });
}

/** 解锁账户 */
export function unlockStaffAccount(id: number) {
  return apiClient<SuccessResponse>(`/staff/auth/accounts/${id}/unlock`, { method: 'POST' });
}

/** 重置用户密码（管理员操作，无需旧密码） */
export function resetStaffAccountPassword(id: number, newPassword: string) {
  return apiClient<SuccessResponse>(`/staff/auth/accounts/${id}/reset-password`, {
    method: 'POST',
    body: { new_password: newPassword } satisfies ResetPasswordRequest,
  });
}

/** 软删除账户（后端禁止删除自己） */
export function deleteStaffAccount(id: number) {
  return apiClient<SuccessResponse>(`/staff/auth/accounts/${id}`, { method: 'DELETE' });
}
