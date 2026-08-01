/**
 * E2E 测试共享工具 — 认证、API 断言、等待策略
 */
import { test as base, expect, type Page, type APIRequestContext } from '@playwright/test'

/** 测试账号凭据（来自 TestAccounts.vue / 后端种子数据） */
export const ACCOUNTS = {
  patient_li: { username: 'patient_li', password: 'password123', portal: 'PATIENT' as const },
  patient_wang: { username: 'patient_wang', password: 'password123', portal: 'PATIENT' as const },
  patient_zhao: { username: 'patient_zhao', password: 'password123', portal: 'PATIENT' as const },
  doctor_zhang: { username: 'doctor_zhang', password: 'password123', portal: 'STAFF' as const },
  doctor_wang: { username: 'doctor_wang', password: 'password123', portal: 'STAFF' as const },
  nurse_liu: { username: 'nurse_liu', password: 'password123', portal: 'STAFF' as const },
  dept_admin: { username: 'dept_admin', password: 'password123', portal: 'STAFF' as const },
  admin: { username: 'admin', password: 'admin', portal: 'STAFF' as const },
} as const

export type AccountKey = keyof typeof ACCOUNTS

/** 通过 API 直接登录，返回 token 并写入 localStorage（跳过 UI 流程，加速测试） */
export async function apiLogin(request: APIRequestContext, account: typeof ACCOUNTS[AccountKey]) {
  // 统一登录端点：/api/auth/login（后端不校验角色，按 role 自动路由）
  const res = await request.post('/api/auth/login', { data: { username: account.username, password: account.password } })
  expect(res.ok(), `登录 ${account.username} 应成功`).toBeTruthy()
  const body = await res.json()
  expect(body.access).toBeTruthy()
  expect(body.refresh).toBeTruthy()
  return { access: body.access as string, refresh: body.refresh as string, user: body.user as Record<string, unknown> }
}

/** 在页面上注入 token（绕过 UI 登录流程） */
export async function injectAuth(page: Page, account: typeof ACCOUNTS[AccountKey]) {
  const { access, refresh, user } = await apiLogin(page.request, account)
  // hn_user 必须一并注入：route-guard 从 localStorage 读取角色，缺失则重定向 /login
  await page.addInitScript(([a, r, u]) => {
    localStorage.setItem('hn_access_token', a)
    localStorage.setItem('hn_refresh_token', r)
    localStorage.setItem('hn_user', u)
  }, [access, refresh, JSON.stringify(user)])
  return { access, refresh }
}

/** 等待页面网络空闲（所有 API 请求完成） */
export async function waitForApiSettled(page: Page) {
  await page.waitForLoadState('networkidle')
}

/** 拦截并记录所有 API 请求（用于断言通信动作是否发生） */
export function captureApiCalls(page: Page) {
  const calls: { method: string; url: string; status: number; body?: unknown }[] = []
  page.on('request', (req) => {
    if (req.url().includes('/api/')) {
      calls.push({ method: req.method(), url: req.url(), status: 0 })
    }
  })
  page.on('response', async (res) => {
    const idx = calls.findIndex((c) => c.url === res.url() && c.status === 0)
    if (idx >= 0) {
      calls[idx].status = res.status()
      try {
        calls[idx].body = await res.json()
      } catch { /* 非 JSON 响应忽略 */ }
    }
  })
  return calls
}

/** 断言某 API 路径被调用过 */
export function expectApiCalled(calls: { url: string }[], pathPattern: string | RegExp) {
  const regex = typeof pathPattern === 'string' ? new RegExp(pathPattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')) : pathPattern
  const matched = calls.filter((c) => regex.test(c.url))
  expect(matched.length, `应调用 API ${pathPattern}`).toBeGreaterThan(0)
  return matched
}

/** 断言某 API 路径未被调用（检测多余的通信） */
export function expectApiNotCalled(calls: { url: string }[], pathPattern: string | RegExp) {
  const regex = typeof pathPattern === 'string' ? new RegExp(pathPattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')) : pathPattern
  const matched = calls.filter((c) => regex.test(c.url))
  expect(matched.length, `不应调用 API ${pathPattern}`).toBe(0)
}

/** 扩展 test fixture：提供已登录的 page */
export const test = base.extend<{
  authedPagePatient: Page
  authedPageStaff: Page
}>({
  authedPagePatient: async ({ page }, use) => {
    await injectAuth(page, ACCOUNTS.patient_li)
    await use(page)
  },
  authedPageStaff: async ({ page }, use) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    await use(page)
  },
})

export { expect }
