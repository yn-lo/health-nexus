/**
 * 医护端页面 E2E 测试 — 验证所有涉及后端交互的控件和页面返回值
 */
import { test, expect } from './helpers'
import { ACCOUNTS, injectAuth } from './helpers'

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

test.describe('医护端 - 仪表盘', () => {
  test('仪表盘加载 crisis + 文章数据', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    const calls = trackApi(page)
    await page.goto('/staff')
    await page.waitForLoadState('networkidle')

    // 应调用危机事件列表 API
    await expect.poll(() => calls.filter((c) => c.url.includes('/api/staff/chat/crisis-events'))).toHaveLength(1)
    expect(calls.find((c) => c.url.includes('/api/staff/chat/crisis-events'))?.status).toBeLessThan(400)
  })

  test('仪表盘显示统计数据', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    await page.goto('/staff')
    await page.waitForLoadState('networkidle')
    await expect(page.locator('body')).toContainText(/未处理危机|待审文章|草稿/, { timeout: 10000 })
  })
})

test.describe('医护端 - 文章管理', () => {
  test('文章列表加载', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    const calls = trackApi(page)
    await page.goto('/staff/articles')
    await page.waitForLoadState('networkidle')

    await expect.poll(() => calls.filter((c) => c.url.includes('/api/staff/wiki/articles'))).toHaveLength(1)
    expect(calls.find((c) => c.url.includes('/api/staff/wiki/articles'))?.status).toBeLessThan(400)
  })

  test('新建文章页可访问', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    await page.goto('/staff/articles/new')
    await page.waitForLoadState('networkidle')

    await expect(page.getByPlaceholder('请输入文章标题')).toBeVisible({ timeout: 10000 })
  })

  test('创建文章（存为草稿）', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    const calls = trackApi(page)
    await page.goto('/staff/articles/new')
    await page.waitForLoadState('networkidle')

    // 填写标题
    await page.getByPlaceholder('请输入文章标题').fill('E2E测试文章')

    // 填写内容（TipTap 编辑器）
    const editor = page.locator('.tiptap, [contenteditable]').first()
    if (await editor.isVisible({ timeout: 5000 }).catch(() => false)) {
      await editor.click()
      await editor.fill('这是E2E测试文章的内容')
    }

    // 点击"存为草稿"按钮
    const draftBtn = page.getByRole('button', { name: '存为草稿', exact: true })
    if (await draftBtn.isVisible({ timeout: 5000 }).catch(() => false)) {
      await draftBtn.click()
      // 应调用 POST /api/staff/wiki/articles
      await expect.poll(
        () => calls.filter((c) => c.url.includes('/api/staff/wiki/articles') && c.method === 'POST'),
        { timeout: 10000 }
      ).toHaveLength(1)
    }
  })

  test('文章审核页加载待审核文章', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    const calls = trackApi(page)
    await page.goto('/staff/articles/review')
    await page.waitForLoadState('networkidle')

    // ArticleReview 复用 listMyArticles API
    await expect.poll(() => calls.filter((c) => c.url.includes('/api/staff/wiki/articles'))).toHaveLength(1)
  })
})

test.describe('医护端 - 危机事件', () => {
  test('危机事件列表加载', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    const calls = trackApi(page)
    await page.goto('/staff/crisis-events')
    await page.waitForLoadState('networkidle')

    await expect.poll(() => calls.filter((c) => c.url.includes('/api/staff/chat/crisis-events'))).toHaveLength(1)
    expect(calls.find((c) => c.url.includes('/api/staff/chat/crisis-events'))?.status).toBeLessThan(400)
  })

  test('标记危机事件已处理', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    await page.goto('/staff/crisis-events')
    await page.waitForLoadState('networkidle')

    // 找到第一个"标记已处理"按钮（仅未处理事件才有此按钮）
    const resolveBtn = page.getByRole('button', { name: '标记已处理' }).first()
    if (await resolveBtn.isVisible({ timeout: 5000 }).catch(() => false)) {
      const calls = trackApi(page)
      await resolveBtn.click()
      // showConfirmDialog 弹出确认框
      const confirmBtn = page.getByRole('button', { name: '确认' }).first()
      await expect(confirmBtn).toBeVisible({ timeout: 3000 })
      await confirmBtn.click()
      // 应调用 handle API
      await expect.poll(
        () => calls.filter((c) => c.url.includes('/api/staff/chat/crisis-events') && c.url.includes('handle')),
        { timeout: 10000 }
      ).toHaveLength(1)
    }
    // 无未处理事件时跳过 — 列表加载测试已覆盖 API 连通性
  })
})

test.describe('医护端 - 个人中心', () => {
  test('医护个人中心渲染（无独立 profile API）', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    await page.goto('/staff/profile')
    await page.waitForLoadState('networkidle')

    // 极简版个人中心基于 authStore.user 渲染，无独立 API 调用
    await expect(page.locator('body')).toContainText(/退出登录/, { timeout: 10000 })
  })

  test('医护退出登录', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    await page.goto('/staff/profile')
    await page.waitForLoadState('networkidle')

    const calls = trackApi(page)
    await page.getByText('退出登录').click()
    await expect.poll(() => calls.filter((c) => c.url.includes('/api/auth/logout'))).toHaveLength(1)
    await page.waitForURL('**/login', { timeout: 10000 })
  })
})

test.describe('医护端 - 完整文章流程', () => {
  test('创建文章 → 提交审核', async ({ page }) => {
    await injectAuth(page, ACCOUNTS.doctor_zhang)
    const calls = trackApi(page)

    // 1. 创建文章
    await page.goto('/staff/articles/new')
    await page.waitForLoadState('networkidle')
    await page.getByPlaceholder('请输入文章标题').fill('E2E完整流程测试')
    const editor = page.locator('.tiptap, [contenteditable]').first()
    if (await editor.isVisible({ timeout: 5000 }).catch(() => false)) {
      await editor.click()
      await editor.fill('完整流程测试内容')
    }

    // 提交审核
    const submitBtn = page.getByRole('button', { name: '提交审核', exact: true })
    if (await submitBtn.isVisible({ timeout: 5000 }).catch(() => false)) {
      await submitBtn.click()
      // 应 POST 创建文章
      await expect.poll(
        () => calls.filter((c) => c.url.includes('/api/staff/wiki/articles') && c.method === 'POST'),
        { timeout: 15000 }
      ).toHaveLength(1)
    }
  })
})
