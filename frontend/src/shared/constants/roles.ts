/**
 * 角色常量集中定义 — 防止字面量散落（AC-ARCH-FE-09）
 *
 * 后端角色值：internal/shared/constants/roles.go
 * 前端必须从本文件导入，禁止在业务代码中硬编码角色字面量
 */

/** 用户角色类型 */
export type UserRole = 'SUPER_ADMIN' | 'DEPT_ADMIN' | 'DOCTOR' | 'NURSE' | 'PATIENT'

/** 管理员角色列表（限科室级配置访问） */
export const ADMIN_ROLES: UserRole[] = ['SUPER_ADMIN', 'DEPT_ADMIN']

/** 超级管理员角色（限全局系统配置访问） */
export const SUPER_ADMIN_ROLE: UserRole = 'SUPER_ADMIN'

/** 医护角色列表 */
export const STAFF_ROLES: UserRole[] = ['SUPER_ADMIN', 'DEPT_ADMIN', 'DOCTOR', 'NURSE']

/** 患者角色列表 */
export const PATIENT_ROLES: UserRole[] = ['PATIENT']

/** 角色 → 中文职称映射（用于 UI 展示） */
export const ROLE_LABEL: Record<UserRole, string> = {
  SUPER_ADMIN: '超级管理员',
  DEPT_ADMIN: '科室管理员',
  DOCTOR: '医生',
  NURSE: '护士',
  PATIENT: '患者',
}

/** 默认职称（角色未知时使用） */
export const DEFAULT_STAFF_LABEL = '医护'
