/**
 * 认证流程 E2E 测试 - 登录、注册、退出
 */
import { test, expect } from './helpers'
import { ACCOUNTS, injectAuth } from './helpers'

test.describe('认证流程', () => {
  test('患者账号密码登录成功', async ({ page }) => {
    const calls: { url: string; status: number }[] = []
    page.on('response', (res) => {
      if (res.url().includes('/api/')) calls.push({ url: res.url(), status: res.status() })
    })

    await page.goto('/login')
    await page.getByPlaceholder('请输入用户名或手机号').fill(ACCOUNTS.patient_li.username)
    await page.getByPlaceholder('请输入密码').fill(ACCOUNTS.patient_li.password)
    await page.getByRole('button', { name: '登 录', exact: true }).click()

    await expect.poll(() => calls.filter((c) => c.url.includes('/api/auth/login'))).toHaveLength(1)
    expect(calls.find((c) => c.url.includes('/api/auth/login'))?.status).toBe(200)
    await page.waitForURL('**/chat', { timeout: 15000 })
    expect(page.url()).toContain('/chat')
  })

  test('医护账号密码登录成功', async ({ page }) => {
    const calls: { url: string; status: number }[] = []
    page.on('response', (res) => {
      if (res.url().includes('/api/')) calls.push({ url: res.url(), status: res.status() })
    })

    await page.goto('/login')
    await page.getByPlaceholder('请输入用户名或手机号').fill(ACCOUNTS.doctor_zhang.username)
    await page.getByPlaceholder('请输入密码').fill(ACCOUNTS.doctor_zhang.password)
    await page.getByRole('button', { name: '登 录', exact: true }).click()

    await expect.poll(() => calls.filter((c) => c.url.includes('/api/auth/login'))).toHaveLength(1)
    expect(calls.find((c) => c.url.includes('/api/auth/login'))?.status).toBe(200)
    await page.waitForURL('**/staff', { timeout: 15000 })
    expect(page.url()).toContain('/staff')
  })

  test('错误密码登录失败显示错误信息', async ({ page }) => {
    await page.goto('/login')
    await page.getByPlaceholder('请输入用户名或手机号').fill(ACCOUNTS.patient_li.username)
    await page.getByPlaceholder('请输入密码').fill('wrongpassword')
    await page.getByRole('button', { name: '登 录', exact: true }).click()

    // 应显示错误提示（role=alert 错误块 和/或 DsToast）
    await expect(page.locator('[role="alert"], .ds-dialog, .ds-toast-icon').first()).toBeVisible({ timeout: 5000 })
    expect(page.url()).toContain('/login')
  })

  test('空表单提交显示验证错误', async ({ page }) => {
    await page.goto('/login')
    await page.getByRole('button', { name: '登 录', exact: true }).click()
    await expect(page.locator('[role="alert"]')).toBeVisible()
    await expect(page.locator('[role="alert"]')).toContainText('请输入用户名和密码')
  })

  test('注册页可访问', async ({ page }) => {
    await page.goto('/chat/register')
    await expect(page.locator('input').first()).toBeVisible()
  })

  test('已登录用户退出登录', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.patient_li)
    await page.goto('/chat/profile')
    await page.waitForLoadState('networkidle')

    const calls: { url: string; status: number }[] = []
    page.on('response', (res) => {
      if (res.url().includes('/api/')) calls.push({ url: res.url(), status: res.status() })
    })

    await page.getByText('退出登录').click()
    await expect.poll(() => calls.filter((c) => c.url.includes('/api/auth/logout'))).toHaveLength(1)
    await page.waitForURL('**/login', { timeout: 10000 })
  })
})
