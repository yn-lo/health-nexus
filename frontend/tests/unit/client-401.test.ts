/**
 * client.ts 401 刷新逻辑单元测试
 * 覆盖：401 触发 refresh → 重试原请求；refresh 失败 → 清 token 跳转；
 *      并发 401 共享单次 refresh；匿名 401（无 token）不刷新；非 401 不刷新。
 *
 * 关键实现细节：
 * - ofetch 的 createFetch() 在模块加载时捕获 globalThis.fetch 引用，
 *   因此每个测试必须在 mock fetch 之后通过 vi.resetModules() + 动态 import 重新加载 client.ts。
 * - ofetch 默认对 5xx 重试 1 次，断言时需考虑这一点。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setTokens, clearTokens, getAccessToken, getRefreshToken } from '@/shared/api/client'
import { mockFetchRouter, jsonRes } from './helpers/mock-fetch'

describe('apiClient 401 刷新逻辑', () => {
  const originalFetch = globalThis.fetch
  const originalLocation = window.location

  beforeEach(() => {
    localStorage.clear()
    // 关键：重置模块缓存，让动态 import 拿到捕获了 mock fetch 的新模块
    vi.resetModules()
    // 拦截 window.location.href 赋值，避免 happy-dom 真实跳转
    // ponytail: happy-dom 的 location 是只读 stub，需替换为可写 mock；保留 pathname 字段以兼容 determineLoginPath
    const mockedLocation = { href: '', pathname: '/' }
    Object.defineProperty(window, 'location', {
      value: mockedLocation,
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
      configurable: true,
    })
    vi.restoreAllMocks()
    vi.resetModules()
  })

  it('401 + 有 refresh token → 调用 /api/auth/refresh 并重试原请求', async () => {
    setTokens('expired-access', 'valid-refresh')
    const router = mockFetchRouter()
    router.addMatcher({
      name: 'protected',
      match: (url) => url.includes('/api/protected'),
      respond: ({ callNumForUrl }) => {
        // callNumForUrl 1：旧 token 返回 401；callNumForUrl 2（重试）：新 token 返回 200
        if (callNumForUrl === 1) return jsonRes(401, { detail: 'token expired' })
        return jsonRes(200, { ok: true })
      },
    })
    router.addMatcher({
      name: 'refresh',
      match: (url) => url.includes('/api/auth/refresh'),
      respond: ({ init }) => {
        // 校验 refresh 请求体包含 refresh token
        const body = typeof init?.body === 'string' ? JSON.parse(init.body) : {}
        expect(body.refresh).toBe('valid-refresh')
        return jsonRes(200, { access: 'new-access', refresh: 'new-refresh' })
      },
    })

    const { apiClient } = await import('@/shared/api/client')
    // 刷新 + 重试成功后正常 resolve，调用方 await 直接拿到重试结果
    const data = await apiClient<{ ok: boolean }>('/protected')

    expect(data).toEqual({ ok: true })
    expect(getAccessToken()).toBe('new-access')
    expect(getRefreshToken()).toBe('new-refresh')
    // 调用顺序：原请求 → refresh → 重试
    expect(router.callsFor((u) => u.includes('/api/protected'))).toBe(2)
    expect(router.callsFor((u) => u.includes('/api/auth/refresh'))).toBe(1)
  })

  it('401 + refresh 失败 -> 清除 token 并跳转 /login', async () => {
    setTokens('expired-access', 'invalid-refresh')
    const router = mockFetchRouter()
    router.addMatcher({
      name: 'protected',
      match: (url) => url.includes('/api/protected'),
      respond: () => jsonRes(401, { detail: 'expired' }),
    })
    router.addMatcher({
      name: 'refresh',
      match: (url) => url.includes('/api/auth/refresh'),
      respond: () => jsonRes(401, { detail: 'invalid refresh token' }),
    })

    const { apiClient } = await import('@/shared/api/client')
    await expect(apiClient('/protected')).rejects.toBeTruthy()

    expect(getAccessToken()).toBeNull()
    expect(getRefreshToken()).toBeNull()
    expect(window.location.href).toBe('/login')
  })

  it('staff 路径下 refresh 失败 -> 跳转统一登录页 /login', async () => {
    setTokens('expired-access', 'invalid-refresh')
    // 覆盖 pathname 以模拟 staff 路径
    Object.defineProperty(window, 'location', {
      value: { href: '', pathname: '/staff/dashboard' },
      writable: true,
      configurable: true,
    })
    const router = mockFetchRouter()
    router.addMatcher({
      name: 'protected',
      match: (url) => url.includes('/api/protected'),
      respond: () => jsonRes(401, {}),
    })
    router.addMatcher({
      name: 'refresh',
      match: (url) => url.includes('/api/auth/refresh'),
      respond: () => jsonRes(401, {}),
    })

    const { apiClient } = await import('@/shared/api/client')
    await expect(apiClient('/protected')).rejects.toBeTruthy()

    expect(window.location.href).toBe('/login')
  })

  it('匿名 401（无任何 token） -> 不尝试 refresh，不跳转', async () => {
    clearTokens()
    const router = mockFetchRouter()
    router.addMatcher({
      name: 'public',
      match: (url) => url.includes('/api/public-endpoint'),
      respond: () => jsonRes(401, { detail: 'auth required' }),
    })

    const { apiClient } = await import('@/shared/api/client')
    await expect(apiClient('/public-endpoint')).rejects.toBeTruthy()

    expect(router.callsFor((u) => u.includes('/api/auth/refresh'))).toBe(0)
    expect(router.callsFor((u) => u.includes('/api/public-endpoint'))).toBe(1)
    expect(getAccessToken()).toBeNull()
    expect(window.location.href).toBe('')
  })

  it('非 401 错误（500）不触发 refresh 流程', async () => {
    setTokens('valid-access', 'valid-refresh')
    const router = mockFetchRouter()
    router.addMatcher({
      name: 'protected',
      match: (url) => url.includes('/api/protected'),
      respond: () => jsonRes(500, { detail: 'server error' }),
    })

    const { apiClient } = await import('@/shared/api/client')
    await expect(apiClient('/protected')).rejects.toBeTruthy()

    // refresh 端点未被调用
    expect(router.callsFor((u) => u.includes('/api/auth/refresh'))).toBe(0)
    // token 未被清除
    expect(getAccessToken()).toBe('valid-access')
    // 未跳转
    expect(window.location.href).toBe('')
  })

  it('并发 401 共享单次 refresh（refresh promise 复用）', async () => {
    setTokens('expired-access', 'valid-refresh')
    const router = mockFetchRouter()
    router.addMatcher({
      name: 'protected',
      match: (url) => url.includes('/api/protected-'),
      respond: ({ callNumForUrl }) => {
        // callNumForUrl 1：原请求 401；callNumForUrl 2：重试 200（按 URL 独立计数）
        if (callNumForUrl === 1) return jsonRes(401, { detail: 'expired' })
        return jsonRes(200, { ok: true })
      },
    })
    router.addMatcher({
      name: 'refresh',
      match: (url) => url.includes('/api/auth/refresh'),
      respond: async () => {
        // 故意延迟，让两个并发 401 都进入 tryRefreshToken 时 refreshPromise 已设置
        await new Promise((r) => setTimeout(r, 50))
        return jsonRes(200, { access: 'new-access', refresh: 'new-refresh' })
      },
    })

    const { apiClient } = await import('@/shared/api/client')
    // 同时发起两个会触发 401 的请求，刷新 + 重试成功后均正常 resolve
    const [r1, r2] = await Promise.all([
      apiClient<{ ok: boolean }>('/protected-a'),
      apiClient<{ ok: boolean }>('/protected-b'),
    ])

    expect(r1).toEqual({ ok: true })
    expect(r2).toEqual({ ok: true })
    // 关键断言：refresh 端点只调用了 1 次（共享）
    expect(router.callsFor((u) => u.includes('/api/auth/refresh'))).toBe(1)
    // 两个原请求各自重试 1 次，总共 4 次（2 原始 + 2 重试）
    expect(router.callsFor((u) => u.includes('/api/protected-'))).toBe(4)
  })
})
