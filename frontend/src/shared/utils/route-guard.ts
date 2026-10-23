import type { NavigationGuardNext, Router, RouteLocationNormalized } from 'vue-router'
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
 * 获取用户角色 — 与令牌同源的 localStorage 持久化值。
 * 登录/注册成功时与令牌一起写入，route-guard 因此不需要依赖 Pinia（shared 不反向依赖 stores）。
 */
function getUserRole(): UserRole | null {
  if (!getAccessToken()) return null
  return (getUserStored()?.role as UserRole) || null
}

/**
 * 跨 MPA 跳转到统一登录页，并保留原目标路径（登录成功后回跳该页）。
 * 不保留 redirect 会导致医护被踢回患者端首页，需重新导航。
 */
function redirectToLogin(to: RouteLocationNormalized): void {
  // ponytail:allow-location 跨 MPA 跳转
  window.location.href = `/login?redirect=${encodeURIComponent(to.fullPath)}`
}

/**
 * 注册全局认证守卫（meta.requiresAuth 路由）
 * @param loginRouteName 未认证时同 SPA 内跳转的路由名；不传则跨 MPA 跳转 /login
 */
export function setupAuthGuards(router: Router, loginRouteName?: string): void {
  router.beforeEach((to) => {
    if (!to.meta.requiresAuth || getAccessToken()) return
    if (loginRouteName) {
      return { name: loginRouteName, query: { redirect: to.fullPath } }
    }
    redirectToLogin(to)
  })
}

/**
 * 医护端路由守卫
 * 只允许医护角色访问，患者角色会被重定向到患者端
 */
export function staffRouteGuard(
  to: RouteLocationNormalized,
  _from: RouteLocationNormalized,
  next: NavigationGuardNext,
): void {
  const userRole = getUserRole()

  // 未登录，跳转到统一登录页（跨 MPA）
  if (!userRole) {
    redirectToLogin(to)
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
  redirectToLogin(to)
}

/**
 * 管理员/超管守卫公共前缀：未登录或非医护角色统一回跳登录页。
 * 返回完成鉴权后的角色；返回 null 表示已处理（调用方应中断）。
 */
function guardStaffAccess(
  to: RouteLocationNormalized,
): UserRole | null {
  const userRole = getUserRole()

  // 未登录，跳转到统一登录页（跨 MPA）
  if (!userRole) {
    redirectToLogin(to)
    return null
  }

  // 非医护角色，跳转到登录页（跨 MPA）
  if (!hasRole(userRole, STAFF_ROLES)) {
    redirectToLogin(to)
    return null
  }

  return userRole
}

/**
 * 创建「医护配置页」角色守卫：通过 guardStaffAccess 前置鉴权后，
 * 非目标角色回退到工作台。admin（科室级）/超级管理员（全局）共用同一守卫体。
 */
function createRoleGuard(isAllowed: (role: UserRole) => boolean) {
  return (
    to: RouteLocationNormalized,
    _from: RouteLocationNormalized,
    next: NavigationGuardNext,
  ): void => {
    const userRole = guardStaffAccess(to)
    if (!userRole) return

    // 非目标角色医护，回退到工作台
    if (!isAllowed(userRole)) {
      next({ name: 'staff-dashboard' })
      return
    }

    next()
  }
}

/**
 * 管理员路由守卫（科室级配置：账号管理 / 科室管理 / 提示词模板）
 * SUPER_ADMIN / DEPT_ADMIN 均可访问；非管理员医护回退到工作台
 */
export const adminRouteGuard = createRoleGuard((role) => hasRole(role, ADMIN_ROLES))

/**
 * 超级管理员路由守卫（全局系统配置：AI 提供商 / RAG / 安全词库 / 安全规则 / 安全话术 / 审计日志）
 * 仅 SUPER_ADMIN 可访问；DEPT_ADMIN 回退到工作台
 */
export const superAdminRouteGuard = createRoleGuard((role) => role === SUPER_ADMIN_ROLE)

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
