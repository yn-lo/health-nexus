/**
 * wiki.ts 接口契约单元测试
 * 覆盖：URL 路径、HTTP 方法、请求体、查询参数、路径参数替换
 *
 * 测试范围（按 shared/api/wiki.ts 实际实现，对齐后端契约 §4 / §5）：
 * - listArticles(params?)               GET    /api/wiki/articles
 * - getArticleDetail(articleId)         GET    /api/wiki/articles/{id}
 * - createArticle(data)                 POST   /api/staff/wiki/articles
 * - listMyArticles(params?)             GET    /api/staff/wiki/articles
 * - getMyArticle(articleId)             GET    /api/staff/wiki/articles/{id}
 * - updateArticle(articleId, data)      PUT    /api/staff/wiki/articles/{id}
 * - submitArticle(articleId)            POST   /api/staff/wiki/articles/{id}/submit
 * - deleteArticle(articleId)            DELETE /api/staff/wiki/articles/{id}
 * - approveArticle(articleId, note?)    POST   /api/staff/wiki/articles/{id}/approve
 * - rejectArticle(articleId, reason)    POST   /api/staff/wiki/articles/{id}/reject
 * - applyReference(data)                POST   /api/staff/wiki/references
 * - listReferences(params?)             GET    /api/staff/wiki/references
 * - approveReference(id, note?)         POST   /api/staff/wiki/references/{id}/approve
 * - rejectReference(id, reason)         POST   /api/staff/wiki/references/{id}/reject
 * - revokeReference(id)                 DELETE /api/staff/wiki/references/{id}
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mockFetchRouter, jsonRes, type MockFetchRouter } from './helpers/mock-fetch'
import { setTokens, clearTokens } from '@/shared/api/client'

describe('wikiApi 接口契约', () => {
  let router: MockFetchRouter

  beforeEach(() => {
    localStorage.clear()
    vi.resetModules()
    setTokens('test-access', 'test-refresh')
    router = mockFetchRouter()
    router.addMatcher({
      name: 'default',
      match: () => true,
      respond: ({ init, url }) => {
        const method = init?.method ?? 'GET'
        if (method === 'DELETE') return jsonRes(200, { success: true })
        if (method === 'POST' || method === 'PUT' || method === 'PATCH') {
          // approve/reject/submit 等无明细返回值，统一返回 success；create/update 返回 ArticleStaff 形状
          if (url.includes('/approve') || url.includes('/reject') || url.includes('/submit')) {
            return jsonRes(200, { success: true })
          }
          return jsonRes(200, {
            id: 1,
            title: 't',
            content: '',
            summary: '',
            cover_url: '',
            status: 'draft',
            version: 1,
            department_id: null,
            department_name: '',
            author_id: 1,
            author_name: '',
            reviewer_id: null,
            review_comment: '',
            view_count: 0,
            allow_reference: false,
            published_at: null,
            created_at: '',
            updated_at: '',
          })
        }
        // GET：列表端点返回分页结构；单端点返回对象
        if (url.includes('/references') || url.endsWith('/articles') || url.includes('/articles?')) {
          return jsonRes(200, { items: [], total: 0, page: 1, page_size: 20 })
        }
        return jsonRes(200, {
          id: 1,
          title: 't',
          content: '',
          summary: '',
          cover_url: '',
          status: 'draft',
          version: 1,
          department_id: null,
          department_name: '',
          author_id: 1,
          author_name: '',
          reviewer_id: null,
          review_comment: '',
          view_count: 0,
          allow_reference: false,
          published_at: null,
          created_at: '',
          updated_at: '',
        })
      },
    })
  })

  afterEach(() => {
    clearTokens()
    vi.restoreAllMocks()
    vi.resetModules()
  })

  // ── 公共接口 ──────────────────────────────────────────────────────
  it('listArticles 无参 → GET /api/wiki/articles（无查询参数）', async () => {
    const { listArticles } = await import('@/shared/api/wiki')
    await listArticles()

    const last = router.lastCall()!
    expect(last.url).toBe('/api/wiki/articles')
    expect(last.init?.method).toBeUndefined()
  })

  it('listArticles({department_id}) → GET /api/wiki/articles?department_id={id}', async () => {
    const { listArticles } = await import('@/shared/api/wiki')
    await listArticles({ department_id: 5 })

    const last = router.lastCall()!
    expect(last.url).toContain('/api/wiki/articles?')
    expect(last.url).toContain('department_id=5')
  })

  it('listArticles({department_id, page, page_size}) → 查询参数完整', async () => {
    const { listArticles } = await import('@/shared/api/wiki')
    await listArticles({ department_id: 5, page: 2, page_size: 10 })

    const last = router.lastCall()!
    expect(last.url).toContain('department_id=5')
    expect(last.url).toContain('page=2')
    expect(last.url).toContain('page_size=10')
  })

  it('getArticleDetail(articleId) → GET /api/wiki/articles/{id}', async () => {
    const { getArticleDetail } = await import('@/shared/api/wiki')
    await getArticleDetail(42)

    const last = router.lastCall()!
    expect(last.url).toBe('/api/wiki/articles/42')
    expect(last.init?.method).toBeUndefined()
  })

  it('listFeaturedArticles({departmentId}) → GET /api/wiki/articles/featured 且带科室参数', async () => {
    const { listFeaturedArticles } = await import('@/shared/api/wiki')
    await listFeaturedArticles(5)

    const last = router.lastCall()!
    expect(last.url).toContain('/api/wiki/articles/featured?')
    expect(last.url).toContain('department_id=5')
    expect(last.init?.method).toBeUndefined()
  })

  // ── 医护端文章接口 ────────────────────────────────────────────────
  it('createArticle(data) → POST /api/staff/wiki/articles 且 body 正确', async () => {
    const { createArticle } = await import('@/shared/api/wiki')
    await createArticle({ title: '糖尿病饮食', content: '<p>正文</p>', summary: '摘要', department_id: 1 })

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/articles')
    expect(last.init?.method).toBe('POST')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ title: '糖尿病饮食', content: '<p>正文</p>', summary: '摘要', department_id: 1 })
  })

  it('listMyArticles() 无参 → GET /api/staff/wiki/articles（无查询参数）', async () => {
    const { listMyArticles } = await import('@/shared/api/wiki')
    await listMyArticles()

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/articles')
    expect(last.init?.method).toBeUndefined()
  })

  it('listMyArticles({status, page, page_size}) → 查询参数正确', async () => {
    const { listMyArticles } = await import('@/shared/api/wiki')
    await listMyArticles({ status: 'pending', page: 1, page_size: 20 })

    const last = router.lastCall()!
    expect(last.url).toContain('/api/staff/wiki/articles?')
    expect(last.url).toContain('status=pending')
    expect(last.url).toContain('page=1')
    expect(last.url).toContain('page_size=20')
  })

  it('getMyArticle(articleId) → GET /api/staff/wiki/articles/{id}', async () => {
    const { getMyArticle } = await import('@/shared/api/wiki')
    await getMyArticle(7)

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/articles/7')
  })

  it('updateArticle(articleId, data) → PUT 且 body 正确', async () => {
    const { updateArticle } = await import('@/shared/api/wiki')
    await updateArticle(3, { title: '新标题', content: '<p>新内容</p>' })

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/articles/3')
    expect(last.init?.method).toBe('PUT')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ title: '新标题', content: '<p>新内容</p>' })
  })

  it('submitArticle(articleId) → POST /api/staff/wiki/articles/{id}/submit', async () => {
    const { submitArticle } = await import('@/shared/api/wiki')
    await submitArticle(10)

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/articles/10/submit')
    expect(last.init?.method).toBe('POST')
  })

  it('deleteArticle(articleId) → DELETE /api/staff/wiki/articles/{id}', async () => {
    const { deleteArticle } = await import('@/shared/api/wiki')
    await deleteArticle(15)

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/articles/15')
    expect(last.init?.method).toBe('DELETE')
  })

  it('setArticleFeatured(articleId, rank) → POST 热门位接口且 body 正确', async () => {
    const { setArticleFeatured } = await import('@/shared/api/wiki')
    await setArticleFeatured(15, 2)

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/articles/15/featured')
    expect(last.init?.method).toBe('POST')
    expect(JSON.parse(last.init?.body as string)).toEqual({ rank: 2 })
  })

  // ── 审核接口 ──────────────────────────────────────────────────────
  it('approveArticle(articleId) 无 note → POST /api/staff/wiki/articles/{id}/approve 无 body', async () => {
    const { approveArticle } = await import('@/shared/api/wiki')
    await approveArticle(8)

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/articles/8/approve')
    expect(last.init?.method).toBe('POST')
    expect(last.init?.body).toBeUndefined()
  })

  it('approveArticle(articleId, note) → POST 且 body 含 note', async () => {
    const { approveArticle } = await import('@/shared/api/wiki')
    await approveArticle(8, '通过')

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/articles/8/approve')
    expect(last.init?.method).toBe('POST')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ note: '通过' })
  })

  it('rejectArticle(articleId, reason) → POST /api/staff/wiki/articles/{id}/reject 且 body 含 reason', async () => {
    const { rejectArticle } = await import('@/shared/api/wiki')
    await rejectArticle(9, '内容不准确')

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/articles/9/reject')
    expect(last.init?.method).toBe('POST')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ reason: '内容不准确' })
  })

  // ── 引用授权接口 ──────────────────────────────────────────────────
  it('applyReference(data) → POST /api/staff/wiki/references 且 body 含 article_id + target_dept_id', async () => {
    const { applyReference } = await import('@/shared/api/wiki')
    await applyReference({ article_id: 1, target_dept_id: 2 })

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/references')
    expect(last.init?.method).toBe('POST')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ article_id: 1, target_dept_id: 2 })
  })

  it('listReferences() 无参 → GET /api/staff/wiki/references（无查询参数）', async () => {
    const { listReferences } = await import('@/shared/api/wiki')
    await listReferences()

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/references')
  })

  it('listReferences({status, direction, page, page_size}) → 查询参数正确', async () => {
    const { listReferences } = await import('@/shared/api/wiki')
    await listReferences({ status: 'pending', direction: 'outgoing', page: 2, page_size: 10 })

    const last = router.lastCall()!
    expect(last.url).toContain('/api/staff/wiki/references?')
    expect(last.url).toContain('status=pending')
    expect(last.url).toContain('direction=outgoing')
    expect(last.url).toContain('page=2')
    expect(last.url).toContain('page_size=10')
  })

  it('approveReference(id, note) → POST 且 body 含 note', async () => {
    const { approveReference } = await import('@/shared/api/wiki')
    await approveReference(5, '同意引用')

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/references/5/approve')
    expect(last.init?.method).toBe('POST')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ note: '同意引用' })
  })

  it('approveReference(id) 无 note → 仍 POST 到 /approve', async () => {
    const { approveReference } = await import('@/shared/api/wiki')
    await approveReference(6)

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/references/6/approve')
    expect(last.init?.method).toBe('POST')
    expect(last.init?.body).toBeUndefined()
  })

  it('rejectReference(id, reason) → POST 且 body 含 reason', async () => {
    const { rejectReference } = await import('@/shared/api/wiki')
    await rejectReference(7, '目标科室不匹配')

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/references/7/reject')
    expect(last.init?.method).toBe('POST')
    const body = JSON.parse(last.init?.body as string)
    expect(body).toEqual({ reason: '目标科室不匹配' })
  })

  it('revokeReference(id) → DELETE /api/staff/wiki/references/{id}', async () => {
    const { revokeReference } = await import('@/shared/api/wiki')
    await revokeReference(8)

    const last = router.lastCall()!
    expect(last.url).toBe('/api/staff/wiki/references/8')
    expect(last.init?.method).toBe('DELETE')
  })
})
