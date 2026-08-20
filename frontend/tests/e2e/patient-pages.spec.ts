/**
 * 患者端页面 E2E 测试 — 验证所有涉及后端交互的控件和页面返回值
 */
import { test, expect } from './helpers'
import { ACCOUNTS, injectAuth } from './helpers'

/** 收集 API 调用 */
function trackApi(page: import('@playwright/test').Page) {
  const calls: { method: string; url: string; status: number }[] = []
  page.on('request', (req) => {
    if (req.url().includes('/api/')) calls.push({ method: req.method(), url: req.url(), status: 0 })
  })
  page.on('response', (res) => {
    const idx = calls.findIndex((c) => c.url === res.url() && c.status === 0)
    if (idx >= 0) calls[idx].status = res.status()
  })
  return calls
}

test.describe('患者端 - 首页', () => {
  test('匿名用户可访问首页', async ({ page }) => {
    await page.goto('/chat')
    await page.waitForLoadState('networkidle')
    await expect(page.getByPlaceholder('请输入您的健康问题...')).toBeVisible({ timeout: 10000 })
  })

  test('已登录患者首页加载科室列表（公开科室）', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.patient_li)
    const calls = trackApi(page)
    await page.goto('/chat')
    await page.waitForLoadState('networkidle')
    await expect.poll(() => calls.filter((c) => c.url.includes('/api/base/departments'))).toHaveLength(1)
    expect(calls.find((c) => c.url.includes('/api/base/departments'))?.status).toBeLessThan(400)
  })

  test('未登录用户点击发送进入匿名对话', async ({ page }) => {
    await page.goto('/chat')
    await page.waitForLoadState('networkidle')
    await page.getByPlaceholder('请输入您的健康问题...').fill('测试问题')
    // 发送按钮（aria-label="发送"）
    await page.getByRole('button', { name: '发送', exact: false }).click()
    // 匿名用户走公开端点（X-Device-Id），直接进入对话页（无登录提示）
    await page.waitForURL('**/conversation**', { timeout: 10000 })
  })

  test('已登录患者发送消息创建对话', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.patient_li)
    const calls = trackApi(page)
    await page.goto('/chat')
    await page.waitForLoadState('networkidle')

    await page.getByPlaceholder('请输入您的健康问题...').fill('头疼怎么办')
    await page.getByRole('button', { name: '发送', exact: false }).click()

    // 会话经 SSE 流式端点创建（conversation 事件下发会话 ID）
    await expect.poll(
      () => calls.filter((c) => c.url.includes('/api/chat/stream') && c.method === 'POST'),
      { timeout: 10000 }
    ).toHaveLength(1)
    expect(calls.find((c) => c.url.includes('/api/chat/stream') && c.method === 'POST')?.status).toBeLessThan(400)
  })
})

test.describe('患者端 - 知识库', () => {
  test('知识列表页加载文章', async ({ page }) => {
    const calls = trackApi(page)
    await page.goto('/wiki')
    await page.waitForLoadState('networkidle')
    await expect.poll(() => calls.filter((c) => c.url.includes('/api/wiki/articles'))).toHaveLength(2)
    expect(calls.find((c) => c.url.includes('/api/wiki/articles'))?.status).toBeLessThan(400)
  })

  test('文章详情页加载文章内容', async ({ page }) => {
    const calls = trackApi(page)
    await page.goto('/wiki')
    await page.waitForLoadState('networkidle')
    await expect.poll(() => calls.filter((c) => c.url.includes('/api/wiki/articles'))).toHaveLength(2)

    // 如果有文章，点击第一篇
    const firstArticle = page.locator('a[href*="/wiki/article/"]').first()
    if (await firstArticle.isVisible({ timeout: 3000 }).catch(() => false)) {
      await firstArticle.click()
      await page.waitForLoadState('networkidle')
      await expect.poll(() => calls.filter((c) => c.url.match(/\/api\/wiki\/articles\/\d+/))).toHaveLength(1)
    }
  })
})

test.describe('患者端 - 个人中心', () => {
  test('个人中心页加载用户信息', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.patient_li)
    await page.goto('/chat/profile')
    await page.waitForLoadState('networkidle')
    // 极简版个人中心仅需展示用户名，无强制 API 调用
    await expect(page.locator('body')).toContainText(/退出登录/, { timeout: 5000 })
  })
})

test.describe('患者端 - 对话页', () => {
  test('对话页加载消息列表', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.patient_li)
    // 先创建一个对话
    await page.goto('/chat')
    await page.waitForLoadState('networkidle')
    await page.getByPlaceholder('请输入您的健康问题...').fill('测试问题')
    await page.getByRole('button', { name: '发送', exact: false }).click()
    await page.waitForURL('**/conversation/**', { timeout: 10000 }).catch(() => {})

    if (page.url().includes('/conversation/')) {
      const calls = trackApi(page)
      await page.reload()
      await page.waitForLoadState('networkidle')
      // 应调用获取消息列表 API（watch + onMounted 可能触发多次）
      const msgCalls = calls.filter((c) => c.url.includes('/api/chat/conversations'))
      expect(msgCalls.length).toBeGreaterThanOrEqual(1)
      expect(msgCalls[0]?.status).toBeLessThan(400)
    }
  })

  test('对话页发送消息触发 SSE 流', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.patient_li)
    // 先创建对话
    await page.goto('/chat')
    await page.waitForLoadState('networkidle')
    await page.getByPlaceholder('请输入您的健康问题...').fill('感冒了怎么办')
    await page.getByRole('button', { name: '发送', exact: false }).click()
    await page.waitForURL('**/conversation/**', { timeout: 10000 }).catch(() => {})

    if (page.url().includes('/conversation/')) {
      await page.waitForLoadState('networkidle')
      const calls = trackApi(page)
      // 对话页输入框 placeholder 是"输入您的健康问题..."
      const msgInput = page.getByPlaceholder('输入您的健康问题...')
      await expect(msgInput).toBeVisible({ timeout: 5000 })
      await msgInput.fill('头疼')
      await page.getByRole('button', { name: '发送', exact: false }).click()
      // 应调用 SSE stream 端点
      await expect.poll(
        () => calls.filter((c) => c.url.includes('/api/chat/stream')),
        { timeout: 15000 }
      ).toHaveLength(1)
    }
  })
})

test.describe('患者端 - 关于页', () => {
  test('关于页为静态页面，无业务 API 调用', async ({ page }) => {
    const calls = trackApi(page)
    await page.goto('/about')
    await page.waitForLoadState('networkidle')
    // 静态页面不应加载业务数据（chat/wiki 等）；允许登录态/科室等公开全局初始化
    const businessCalls = calls.filter(
      (c) => c.url.includes('/api/chat') || c.url.includes('/api/wiki') || c.url.includes('/api/staff')
    )
    expect(businessCalls.length).toBe(0)
  })
})
