import type { NavigationGuardNext, RouteLocationNormalized } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { getAccessToken, getUserStored } from '@/shared/api/client'
import {
  ADMIN_ROLES,
  STAFF_ROLES,
  PATIENT_ROLES,
  SUPER_ADMIN_ROLE,
  type UserRole,
} from '@/shared/constants/roles'

/**
 * 检查用户是否具有指定角色
 * @param userRole 用户当前角色
 * @param allowedRoles 允许的角色列表
 * @returns 是否有权限
 */
function hasRole(userRole: UserRole, allowedRoles: UserRole[]): boolean {
  return allowedRoles.includes(userRole)
}

/**
 * 获取用户角色 - 优先读 Pinia store，回退到 localStorage 持久化值
 * 用户信息在登录时已持久化（stores/auth.ts），页面刷新后 store 自动从中初始化，
 * 因此无需再调用 /auth/settings/profile（后端未提供该端点）。
 */
function getUserRole(): UserRole | null {
  if (!getAccessToken()) return null
  const authStore = useAuthStore()
  return (authStore.user?.role as UserRole) || (getUserStored()?.role as UserRole) || null
}

/**
 * 医护端路由守卫
 * 只允许医护角色访问，患者角色会被重定向到患者端
 */
export function staffRouteGuard(
  _to: RouteLocationNormalized,
  _from: RouteLocationNormalized,
  next: NavigationGuardNext,
): void {
  const userRole = getUserRole()

  // 未登录，跳转到统一登录页（跨 MPA）
  if (!userRole) {
    window.location.href = '/login' // ponytail:allow-location 跨 MPA 跳转
    return
  }

  // 医护角色，允许访问
  if (hasRole(userRole, STAFF_ROLES)) {
    next()
    return
  }

  // 患者角色，重定向到患者端（跨 MPA）
  if (hasRole(userRole, PATIENT_ROLES)) {
    window.location.href = '/chat' // ponytail:allow-location 跨 MPA 跳转
    return
  }

  // 其他情况，跳转到登录页（跨 MPA）
  window.location.href = '/login' // ponytail:allow-location 跨 MPA 跳转
}

/**
 * 管理员路由守卫（科室级配置：账号管理 / 科室管理 / 提示词模板）
 * SUPER_ADMIN / DEPT_ADMIN 均可访问；非管理员医护回退到工作台
 */
export function adminRouteGuard(
  _to: RouteLocationNormalized,
  _from: RouteLocationNormalized,
  next: NavigationGuardNext,
): void {
  const userRole = getUserRole()

  // 未登录，跳转到统一登录页（跨 MPA）
  if (!userRole) {
    window.location.href = '/login' // ponytail:allow-location 跨 MPA 跳转
    return
  }

  // 非医护角色，跳转到登录页（跨 MPA）
  if (!hasRole(userRole, STAFF_ROLES)) {
    window.location.href = '/login' // ponytail:allow-location 跨 MPA 跳转
    return
  }

  // 非管理员医护，回退到工作台
  if (!hasRole(userRole, ADMIN_ROLES)) {
    next({ name: 'staff-dashboard' })
    return
  }

  next()
}

/**
 * 超级管理员路由守卫（全局系统配置：AI 提供商 / RAG / 安全词库 / 安全规则 / 安全话术 / 审计日志）
 * 仅 SUPER_ADMIN 可访问；DEPT_ADMIN 回退到工作台
 */
export function superAdminRouteGuard(
  _to: RouteLocationNormalized,
  _from: RouteLocationNormalized,
  next: NavigationGuardNext,
): void {
  const userRole = getUserRole()

  if (!userRole) {
    window.location.href = '/login' // ponytail:allow-location 跨 MPA 跳转
    return
  }

  if (!hasRole(userRole, STAFF_ROLES)) {
    window.location.href = '/login' // ponytail:allow-location 跨 MPA 跳转
    return
  }

  if (userRole !== SUPER_ADMIN_ROLE) {
    next({ name: 'staff-dashboard' })
    return
  }

  next()
}

/**
 * 患者端路由守卫
 * 允许未登录用户访问公开页面；meta.requiresAuth 路由强制登录
 * 医护角色访问身份敏感路由（/chat 咨询流）时重定向到医护端；
 * meta.allowStaffPreview 的公开内容路由（/wiki）允许医护只读预览；
 * 符合 REQ-AUTH-004 公开访问规则
 */
export function patientRouteGuard(
  to: RouteLocationNormalized,
  _from: RouteLocationNormalized,
  next: NavigationGuardNext,
): void {
  const userRole = getUserRole()

  // 需要登录的路由（如 /chat/profile），未登录时跳转 /login 并保留 redirect
  if (to.meta.requiresAuth && !userRole) {
    next({ name: 'login', query: { redirect: to.fullPath } })
    return
  }

  // 未登录，允许访问公开页面
  if (!userRole) {
    next()
    return
  }

  // 患者角色，允许访问
  if (hasRole(userRole, PATIENT_ROLES)) {
    next()
    return
  }

  // 医护角色：公开内容路由（allowStaffPreview）放行只读预览，其余重定向到医护端（跨 MPA）
  if (hasRole(userRole, STAFF_ROLES)) {
    if (to.meta.allowStaffPreview) {
      next()
      return
    }
    window.location.href = '/staff' // ponytail:allow-location 跨 MPA 跳转
    return
  }

  // 其他情况，允许访问
  next()
}
