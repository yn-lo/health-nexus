/**
 * chat.ts 接口契约单元测试
 * 覆盖：URL 路径、HTTP 方法、请求体、查询参数、路径参数替换
 *
 * 测试范围（按 shared/api/chat.ts 实际实现，对齐后端契约 §3.1~3.6）：
 * - listConversations(params?)         GET    /api/chat/conversations
 * - getConversation(id)                GET    /api/chat/conversations/{id}
 * - updateConversation(id, data)       PATCH  /api/chat/conversations/{id}
 * - deleteConversation(id)             DELETE /api/chat/conversations/{id}
 * - listMessages(convId, params?)      GET    /api/chat/conversations/{convId}/messages
 *
 * 注：后端 chat 域无 POST /conversations（新会话由 SSE 隐式创建）、无 feedback 端点；
 *     SSE 端点由 useSSEChat.test.ts 覆盖。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mockFetchRouter, jsonRes, type MockFetchRouter } from './helpers/mock-fetch'
import { setTokens, clearTokens } from '@/shared/api/client'

describe('chatApi 接口契约', () => {
  let router: MockFetchRouter

  beforeEach(() => {
    localStorage.clear()
    vi.resetModules()
    setTokens('test-access', 'test-refresh')
    router = mockFetchRouter()
    // 默认 matcher：根据方法/URL 返回合适的空响应
    router.addMatcher({
      name: 'default',
      match: () => true,
      respond: ({ init, url }) => {
        const method = init?.method ?? 'GET'
        if (method === 'DELETE') return jsonRes(200, { success: true })
        if (method === 'PATCH' || method === 'PUT') {
          // 会话更新返回 ConversationResponse 形状
          return jsonRes(200, {
            id: 'conv-1',
            title: 't',
            locked_dept_id: null,
            archived: false,
            last_message_at: null,
            created_at: '',
          })
        }
        // GET：会话列表返回分页；消息列表返回数组；单会话返回对象
        if (url.endsWith('/conversations') || url.includes('/conversations?')) {
          return jsonRes(200, { items: [], total: 0, page: 1, page_size: 20 })
        }
        if (url.includes('/messages')) {
          return jsonRes(200, [])
        }
        // 单会话详情
        return jsonRes(200, {
          id: 'conv-1',
          title: 't',
          locked_dept_id: null,
          archived: false,
          last_message_at: null,
          created_at: '',
        })
      },
    })
  })

  afterEach(() => {
    clearTokens()
    vi.restoreAllMocks()
    vi.resetModules()
  })

  // ── listConversations ─────────────────────────────────────────────
  it('listConversations 无参 → GET /api/chat/conversations（无查询参数）', async () => {
    const { listConversations } = await import('@/shared/api/chat')
    await listConversations()

    const last = router.lastCall()!
    expect(last.url).toBe('/api/chat/conversations')
    // GET 请求 ofetch 不设置 method，init.method 为 undefined
    expect(last.init?.method).toBeUndefined()
  })

  it('listConversations({archived, page, page_size}) → 查询参数正确', async () => {
    const { listConversations } = await import('@/shared/api/chat')
    await listConversations({ archived: true, page: 2, page_size: 10 })

    const last = router.lastCall()!
    expect(last.url).toContain('/api/chat/conversations?')
    expect(last.url).toContain('archived=true')
    expect(last.url).toContain('page=2')
    expect(last.url).toContain('page_size=10')
  })

  // ── getConversation ───────────────────────────────────────────────
  it('getConversation(id) → GET /api/chat/conversations/{id}，路径参数替换正确', async () => {
    const { getConversation } = await import('@/shared/api/chat')
    await getConversation('conv-abc')

    const last = router.lastCall()!
    expect(last.url).toBe('/api/chat/conversations/conv-abc')
    expect(last.init?.method).toBeUndefined()
  })

  // ── updateConversation ────────────────────────────────────────────
  it('updateConversation(id, {title}) → PATCH 且 body 含 title', async () => {
    const { updateConversation } = await import('@/shared/api/chat')
    await updateConversation('conv-1', { title: '新标题' })

    const last = router.lastCall()!
    expect(last.url).toBe('/api/chat/conversations/conv-1')
    expect(last.init?.method).toBe('PATCH')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ title: '新标题' })
  })

  it('updateConversation(id, {archived}) → PATCH 且 body 含 archived', async () => {
    const { updateConversation } = await import('@/shared/api/chat')
    await updateConversation('conv-2', { archived: true })

    const last = router.lastCall()!
    expect(last.url).toBe('/api/chat/conversations/conv-2')
    expect(last.init?.method).toBe('PATCH')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ archived: true })
  })

  // ── deleteConversation ────────────────────────────────────────────
  it('deleteConversation(id) → DELETE /api/chat/conversations/{id}', async () => {
    const { deleteConversation } = await import('@/shared/api/chat')
    await deleteConversation('conv-3')

    const last = router.lastCall()!
    expect(last.url).toBe('/api/chat/conversations/conv-3')
    expect(last.init?.method).toBe('DELETE')
  })

  // ── listMessages ──────────────────────────────────────────────────
  it('listMessages(convId) 无参 → GET /api/chat/conversations/{convId}/messages（无查询参数）', async () => {
    const { listMessages } = await import('@/shared/api/chat')
    await listMessages('conv-1')

    const last = router.lastCall()!
    expect(last.url).toBe('/api/chat/conversations/conv-1/messages')
    expect(last.init?.method).toBeUndefined()
  })

  it('listMessages(convId, {limit, before}) → 游标分页查询参数正确', async () => {
    const { listMessages } = await import('@/shared/api/chat')
    await listMessages('conv-1', { limit: 50, before: 'msg-uuid-xyz' })

    const last = router.lastCall()!
    expect(last.url).toContain('/api/chat/conversations/conv-1/messages?')
    expect(last.url).toContain('limit=50')
    expect(last.url).toContain('before=msg-uuid-xyz')
  })
})
