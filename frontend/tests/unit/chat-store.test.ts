/**
 * chat Pinia store 单元测试
 * 覆盖：list/get/update/delete/messages 状态同步逻辑
 *
 * 对齐 stores/chat.ts：store 已移除 createConversation action（后端无 POST /conversations，
 * 新会话由 SSE 隐式创建），无 submitFeedback（后端无 feedback 端点）。
 */
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import type { Conversation, Message } from '@/shared/types/chat'

/** 构造测试用 Conversation — 对齐后端 ConversationResponse */
function makeConv(overrides: Partial<Conversation> = {}): Conversation {
  return {
    id: 'conv-1',
    title: '测试会话',
    locked_dept_id: null,
    archived: false,
    last_message_at: null,
    created_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

/** 构造测试用 Message — 对齐后端 MessageResponse */
function makeMsg(id: string, content: string): Message {
  return {
    id,
    conversation_id: 'conv-1',
    role: 'assistant',
    content,
    result_code: 'ANSWERED',
    references: [],
    created_at: '2026-01-01T00:00:00Z',
  }
}

// mock chat API 模块（仅包含 store 实际依赖的函数）
vi.mock('@/shared/api/chat', () => ({
  listConversations: vi.fn(),
  getConversation: vi.fn(),
  updateConversation: vi.fn(),
  deleteConversation: vi.fn(),
  listMessages: vi.fn(),
}))

import * as chatApi from '@/shared/api/chat'
import { useChatStore } from '@/stores/chat'

const apiMocks = chatApi as unknown as Record<string, ReturnType<typeof vi.fn>>

describe('useChatStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    for (const key of Object.keys(apiMocks)) {
      apiMocks[key].mockReset()
    }
  })

  it('fetchConversations 填充会话列表（解包 items）', async () => {
    apiMocks.listConversations.mockResolvedValue({
      items: [makeConv({ id: 'a' }), makeConv({ id: 'b' })],
      total: 2,
      page: 1,
      page_size: 20,
    })
    const store = useChatStore()

    await store.fetchConversations()

    expect(store.conversations).toHaveLength(2)
    expect(store.conversations[0].id).toBe('a')
    expect(store.loading).toBe(false)
  })

  it('fetchConversations 失败时 loading 复位', async () => {
    apiMocks.listConversations.mockRejectedValue(new Error('boom'))
    const store = useChatStore()

    await expect(store.fetchConversations()).rejects.toThrow('boom')
    expect(store.loading).toBe(false)
  })

  it('fetchConversation 命中已存在会话时原地替换', async () => {
    const fetched = makeConv({ id: 'c1', title: '更新后标题', archived: true })
    apiMocks.getConversation.mockResolvedValue(fetched)
    const store = useChatStore()
    store.conversations.push(makeConv({ id: 'c1', title: '旧标题' }))

    const result = await store.fetchConversation('c1')

    expect(result.archived).toBe(true)
    expect(store.conversations).toHaveLength(1)
    expect(store.conversations[0].title).toBe('更新后标题')
    expect(store.currentConversation?.id).toBe('c1')
  })

  it('fetchConversation 列表中不存在时前置插入', async () => {
    const fetched = makeConv({ id: 'c2', title: '新加载的' })
    apiMocks.getConversation.mockResolvedValue(fetched)
    const store = useChatStore()

    await store.fetchConversation('c2')

    expect(store.conversations).toHaveLength(1)
    expect(store.conversations[0].id).toBe('c2')
  })

  it('updateConversation 同步更新列表和 currentConversation', async () => {
    const updated = makeConv({ id: 'u1', title: '重命名后', archived: true })
    apiMocks.updateConversation.mockResolvedValue(updated)
    const store = useChatStore()
    store.conversations.push(makeConv({ id: 'u1', title: '重命名前' }))
    store.currentConversation = makeConv({ id: 'u1', title: '重命名前' })

    const result = await store.updateConversation('u1', { title: '重命名后' })

    expect(apiMocks.updateConversation).toHaveBeenCalledWith('u1', { title: '重命名后' })
    expect(result.title).toBe('重命名后')
    expect(store.conversations[0].title).toBe('重命名后')
    expect(store.currentConversation?.title).toBe('重命名后')
  })

  it('updateConversation 当 currentConversation 不是该 id 时不被改动', async () => {
    const updated = makeConv({ id: 'u2', title: 'X' })
    apiMocks.updateConversation.mockResolvedValue(updated)
    const store = useChatStore()
    store.currentConversation = makeConv({ id: 'other' })

    await store.updateConversation('u2', { archived: true })

    expect(store.currentConversation?.id).toBe('other')
  })

  it('deleteConversation 从列表移除并清空 current（命中）', async () => {
    apiMocks.deleteConversation.mockResolvedValue({ success: true })
    const store = useChatStore()
    store.conversations.push(makeConv({ id: 'd1' }), makeConv({ id: 'd2' }))
    store.currentConversation = makeConv({ id: 'd1' })
    store.messages.push(makeMsg('m1', 'msg'))

    await store.deleteConversation('d1')

    expect(store.conversations).toHaveLength(1)
    expect(store.conversations[0].id).toBe('d2')
    expect(store.currentConversation).toBeNull()
    expect(store.messages).toHaveLength(0)
  })

  it('deleteConversation 不命中 current 时只删列表', async () => {
    apiMocks.deleteConversation.mockResolvedValue({ success: true })
    const store = useChatStore()
    store.conversations.push(makeConv({ id: 'd1' }), makeConv({ id: 'd2' }))
    store.currentConversation = makeConv({ id: 'd2' })
    store.messages.push(makeMsg('m1', 'msg'))

    await store.deleteConversation('d1')

    expect(store.conversations).toHaveLength(1)
    expect(store.currentConversation?.id).toBe('d2')
    expect(store.messages).toHaveLength(1)
  })

  it('fetchMessages 填充消息列表', async () => {
    const msgs = [makeMsg('m1', 'a'), makeMsg('m2', 'b')]
    apiMocks.listMessages.mockResolvedValue(msgs)
    const store = useChatStore()

    await store.fetchMessages('conv-1')

    expect(store.messages).toHaveLength(2)
    expect(store.messages[1].content).toBe('b')
    expect(store.loading).toBe(false)
  })

  it('fetchMessages 失败时 loading 复位', async () => {
    apiMocks.listMessages.mockRejectedValue(new Error('boom'))
    const store = useChatStore()

    await expect(store.fetchMessages('conv-1')).rejects.toThrow('boom')
    expect(store.loading).toBe(false)
  })

  it('addMessage 追加到列表末尾', () => {
    const store = useChatStore()
    store.messages.push(makeMsg('m1', 'a'))

    store.addMessage(makeMsg('m2', 'b'))

    expect(store.messages).toHaveLength(2)
    expect(store.messages[1].id).toBe('m2')
  })

  it('$reset 清空所有状态（登出场景）', async () => {
    apiMocks.listConversations.mockResolvedValue({
      items: [makeConv({ id: 'a' })],
      total: 1, page: 1, page_size: 20,
    })
    apiMocks.listMessages.mockResolvedValue([makeMsg('m1', 'msg')])
    const store = useChatStore()
    await store.fetchConversations()
    await store.fetchMessages('conv-1')
    store.currentConversation = makeConv({ id: 'a' })

    store.$reset()

    expect(store.conversations).toHaveLength(0)
    expect(store.currentConversation).toBeNull()
    expect(store.messages).toHaveLength(0)
    expect(store.loading).toBe(false)
    // fetchEpoch 重置后，新的 fetchMessages 不会误判为 stale
    await store.fetchMessages('conv-2')
    expect(store.messages).toHaveLength(1)
  })
})
