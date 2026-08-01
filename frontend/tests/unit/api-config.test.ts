/**
 * config.ts 接口契约单元测试
 * 覆盖：URL 路径、HTTP 方法、请求体、查询参数
 *
 * 测试范围（按 shared/api/config.ts 实际实现，覆盖每类 CRUD 模式代表性端点）：
 * - listAIProviders()                  GET    /api/staff/config/ai-providers
 * - createAIProvider(data)              POST   /api/staff/config/ai-providers
 * - updateAIProvider(id, data)         PUT    /api/staff/config/ai-providers/{id}
 * - deleteAIProvider(id)                DELETE /api/staff/config/ai-providers/{id}
 * - listSensitiveWords({page,page_size}) GET  /api/staff/config/sensitive-words
 * - getRAGConfig()                      GET    /api/staff/config/rag
 * - updateRAGConfig(data)               PUT    /api/staff/config/rag
 * - getSafetyMessages()                 GET    /api/staff/config/safety-messages
 * - listPromptTemplates({type,is_active,page,page_size}) GET /api/staff/config/prompts
 *
 * 完整 CRUD 模式覆盖：list/create/update/delete（AIProvider）+ 单例读写（RAG/SafetyMessages）+ 列表分页（SensitiveWords/PromptTemplates）。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mockFetchRouter, jsonRes, type MockFetchRouter } from './helpers/mock-fetch'
import { setTokens, clearTokens } from '@/shared/api/client'

describe('configApi 接口契约', () => {
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
        if (method === 'DELETE') return jsonRes(200, { success: true })
        if (method === 'POST' || method === 'PUT' || method === 'PATCH') {
          return jsonRes(200, { id: 1 })
        }
        // GET 默认返回数组（部分端点返回对象，由具体测试覆盖）
        return jsonRes(200, [])
      },
    })
  })

  afterEach(() => {
    clearTokens()
    vi.restoreAllMocks()
    vi.resetModules()
  })

  // ── 6.1 AI 提供商（完整 CRUD 模式）──────────────────────────────
  it('listAIProviders() → GET /api/staff/config/ai-providers', async () => {
    const { listAIProviders } = await import('@/shared/api/config')
    await listAIProviders()

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/config/ai-providers')
    expect(last.init?.method).toBeUndefined()
  })

  it('listAIProviders({provider_type, is_active}) → 查询参数正确', async () => {
    const { listAIProviders } = await import('@/shared/api/config')
    await listAIProviders({ provider_type: 'llm', is_active: true })

    const last = router.lastCall()!
    expect(last.url).toContain('/api/staff/config/ai-providers?')
    expect(last.url).toContain('provider_type=llm')
    expect(last.url).toContain('is_active=true')
  })

  it('createAIProvider(data) → POST 且 body 正确', async () => {
    const { createAIProvider } = await import('@/shared/api/config')
    await createAIProvider({
      provider_type: 'embedding',
      name: 'text-embedding-3',
      api_base: 'https://api.example.com',
      api_key: 'sk-xxx',
      model_name: 'text-embedding-3-small',
      dimension: 1536,
    })

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/config/ai-providers')
    expect(last.init?.method).toBe('POST')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({
      provider_type: 'embedding',
      name: 'text-embedding-3',
      api_base: 'https://api.example.com',
      api_key: 'sk-xxx',
      model_name: 'text-embedding-3-small',
      dimension: 1536,
    })
  })

  it('updateAIProvider(id, data) → PUT /api/staff/config/ai-providers/{id} 且 body 正确', async () => {
    const { updateAIProvider } = await import('@/shared/api/config')
    await updateAIProvider(7, { is_active: false, name: 'new-name' })

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/config/ai-providers/7')
    expect(last.init?.method).toBe('PUT')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ is_active: false, name: 'new-name' })
  })

  it('deleteAIProvider(id) → DELETE /api/staff/config/ai-providers/{id}', async () => {
    const { deleteAIProvider } = await import('@/shared/api/config')
    await deleteAIProvider(9)

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/config/ai-providers/9')
    expect(last.init?.method).toBe('DELETE')
  })

  // ── 6.2 敏感词（分页 GET）──────────────────────────────────────
  it('listSensitiveWords() 无参 → GET /api/staff/config/sensitive-words', async () => {
    const { listSensitiveWords } = await import('@/shared/api/config')
    await listSensitiveWords()

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/config/sensitive-words')
    expect(last.init?.method).toBeUndefined()
  })

  it('listSensitiveWords({page, page_size}) → 分页参数正确', async () => {
    const { listSensitiveWords } = await import('@/shared/api/config')
    await listSensitiveWords({ page: 3, page_size: 50 })

    const last = router.lastCall()!
    expect(last.url).toContain('/api/staff/config/sensitive-words?')
    expect(last.url).toContain('page=3')
    expect(last.url).toContain('page_size=50')
  })

  it('listSensitiveWords({category, page, page_size}) → 全部参数附加', async () => {
    const { listSensitiveWords } = await import('@/shared/api/config')
    await listSensitiveWords({ category: 'suicide', page: 1, page_size: 20 })

    const last = router.lastCall()!
    expect(last.url).toContain('category=suicide')
    expect(last.url).toContain('page=1')
    expect(last.url).toContain('page_size=20')
  })

  // ── 6.4 RAG 配置（单例读写）────────────────────────────────────
  it('getRAGConfig() → GET /api/staff/config/rag', async () => {
    const { getRAGConfig } = await import('@/shared/api/config')
    await getRAGConfig()

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/config/rag')
    expect(last.init?.method).toBeUndefined()
  })

  it('updateRAGConfig(data) → PUT /api/staff/config/rag 且 body 正确', async () => {
    const { updateRAGConfig } = await import('@/shared/api/config')
    await updateRAGConfig({ chunk_size: 800, top_k: 10, rerank_enabled: true })

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/config/rag')
    expect(last.init?.method).toBe('PUT')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ chunk_size: 800, top_k: 10, rerank_enabled: true })
  })

  // ── 6.5 安全话术（单例只读代表）────────────────────────────────
  it('getSafetyMessages() → GET /api/staff/config/safety-messages', async () => {
    const { getSafetyMessages } = await import('@/shared/api/config')
    await getSafetyMessages()

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/config/safety-messages')
    expect(last.init?.method).toBeUndefined()
  })

  // ── 6.6 Prompt 模板（分页 + 多查询参数）────────────────────────
  it('listPromptTemplates() 无参 → GET /api/staff/config/prompts', async () => {
    const { listPromptTemplates } = await import('@/shared/api/config')
    await listPromptTemplates()

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/config/prompts')
  })

  it('listPromptTemplates({type, is_active, page, page_size}) → 查询参数正确', async () => {
    const { listPromptTemplates } = await import('@/shared/api/config')
    await listPromptTemplates({ type: 'system', is_active: true, page: 1, page_size: 20 })

    const last = router.lastCall()!
    expect(last.url).toContain('/api/staff/config/prompts?')
    expect(last.url).toContain('type=system')
    expect(last.url).toContain('is_active=true')
    expect(last.url).toContain('page=1')
    expect(last.url).toContain('page_size=20')
  })
})
