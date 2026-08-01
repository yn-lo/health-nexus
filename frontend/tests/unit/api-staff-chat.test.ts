/**
 * staffChat.ts 接口契约单元测试
 * 覆盖：URL 路径、HTTP 方法、请求体、查询参数
 *
 * 测试范围（按 shared/api/staffChat.ts 实际实现）：
 * - listCrisisEvents(params?)           GET   /api/staff/chat/crisis-events
 * - handleCrisisEvent(eventId, data?)   POST  /api/staff/chat/crisis-events/{eventId}/handle
 *
 * 对齐 api-contracts.md §3.7 / §3.8。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mockFetchRouter, jsonRes, type MockFetchRouter } from './helpers/mock-fetch'
import { setTokens, clearTokens } from '@/shared/api/client'

describe('staffChatApi 接口契约', () => {
  let router: MockFetchRouter

  beforeEach(() => {
    localStorage.clear()
    vi.resetModules()
    setTokens('test-access', 'test-refresh')
    router = mockFetchRouter()
    router.addMatcher({
      name: 'default',
      match: () => true,
      respond: ({ init }) => {
        const method = init?.method ?? 'GET'
        if (method === 'POST') return jsonRes(200, { success: true })
        // GET crisis-events 返回分页结构
        return jsonRes(200, { items: [], total: 0, page: 1, page_size: 20 })
      },
    })
  })

  afterEach(() => {
    clearTokens()
    vi.restoreAllMocks()
    vi.resetModules()
  })

  // ── listCrisisEvents ──────────────────────────────────────────────
  it('listCrisisEvents() 无参 → GET /api/staff/chat/crisis-events（无查询参数）', async () => {
    const { listCrisisEvents } = await import('@/shared/api/staffChat')
    await listCrisisEvents()

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/chat/crisis-events')
    expect(last.init?.method).toBeUndefined()
  })

  it('listCrisisEvents({handled, level, page, page_size}) → 查询参数正确', async () => {
    const { listCrisisEvents } = await import('@/shared/api/staffChat')
    await listCrisisEvents({ handled: false, level: 'high', page: 2, page_size: 10 })

    const last = router.lastCall()!
    expect(last.url).toContain('/api/staff/chat/crisis-events?')
    expect(last.url).toContain('handled=false')
    expect(last.url).toContain('level=high')
    expect(last.url).toContain('page=2')
    expect(last.url).toContain('page_size=10')
  })

  it('listCrisisEvents({handled: true}) → 仅附加 handled=true', async () => {
    const { listCrisisEvents } = await import('@/shared/api/staffChat')
    await listCrisisEvents({ handled: true })

    const last = router.lastCall()!
    expect(last.url).toContain('handled=true')
    expect(last.url).not.toContain('level=')
    expect(last.url).not.toContain('page=')
  })

  // ── handleCrisisEvent ─────────────────────────────────────────────
  it('handleCrisisEvent(eventId, {note}) → POST 且 body 含 note', async () => {
    const { handleCrisisEvent } = await import('@/shared/api/staffChat')
    await handleCrisisEvent(123, { note: '已联系精神科' })

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/chat/crisis-events/123/handle')
    expect(last.init?.method).toBe('POST')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ note: '已联系精神科' })
  })

  it('handleCrisisEvent(eventId) 无 note → 仍 POST 到 /handle', async () => {
    const { handleCrisisEvent } = await import('@/shared/api/staffChat')
    await handleCrisisEvent(456)

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/chat/crisis-events/456/handle')
    expect(last.init?.method).toBe('POST')
  })

  it('handleCrisisEvent 路径参数替换正确（不同 eventId 不同 URL）', async () => {
    const { handleCrisisEvent } = await import('@/shared/api/staffChat')
    await handleCrisisEvent(789)

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/chat/crisis-events/789/handle')
  })
})
