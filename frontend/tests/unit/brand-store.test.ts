/**
 * 品牌 Store 单元测试
 * 覆盖：默认回退、标题前缀、接口拉取覆盖、失败降级、ensureLoaded 幂等
 */
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useBrandStore } from '@/stores/brand'
import type { BrandConfig } from '@/shared/types/config'

const mockGetPublic = vi.fn()

vi.mock('@/shared', () => ({
  configApi: { getPublicBrandConfig: (...args: unknown[]) => mockGetPublic(...args) },
}))

const FETCHED: BrandConfig = {
  name: '某某医院',
  wordmark: 'XX HOSPITAL',
  org_name: '某某市第一人民医院',
  slogan: '智能健康 宣教平台',
  site_url: 'https://hospital.example.com',
  logo_url: '/uploads/logo.png',
  site_title: '某某医院健康教育',
  favicon_url: '/uploads/favicon.png',
  updated_at: '2026-01-01T00:00:00Z',
}

describe('useBrandStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockGetPublic.mockReset()
    document.title = ''
  })

  it('初始使用兜底默认值，logo 回退到内置图', () => {
    const store = useBrandStore()
    expect(store.brand.name).toBe('Health Nexus')
    expect(store.brand.site_title).toBe('健康宣教平台')
    expect(store.logoUrl).toContain('hn_logo')
  })

  it('ensureLoaded 拉取后覆盖品牌，并按前缀应用浏览器标题', async () => {
    mockGetPublic.mockResolvedValue(FETCHED)
    const store = useBrandStore()
    store.setTitlePrefix('患者端')
    await store.ensureLoaded()

    expect(store.brand.name).toBe('某某医院')
    expect(store.logoUrl).toBe('/uploads/logo.png')
    expect(document.title).toBe('患者端 - 某某医院健康教育')
  })

  it('接口失败时保留默认值且不抛错', async () => {
    mockGetPublic.mockRejectedValue(new Error('network'))
    const store = useBrandStore()
    await store.ensureLoaded()
    expect(store.brand.name).toBe('Health Nexus')
  })

  it('ensureLoaded 幂等：并发调用复用同一请求', async () => {
    mockGetPublic.mockResolvedValue(FETCHED)
    const store = useBrandStore()
    await Promise.all([store.ensureLoaded(), store.ensureLoaded()])
    expect(mockGetPublic).toHaveBeenCalledTimes(1)
  })

  it('空品牌名/字标/站点标题回退默认值（避免空白品牌与空标题）', async () => {
    const empty = { ...FETCHED, name: '', wordmark: '', site_title: '' }
    mockGetPublic.mockResolvedValue(empty)
    const store = useBrandStore()
    await store.ensureLoaded()
    expect(store.brand.name).toBe('Health Nexus')
    expect(store.brand.wordmark).toBe('HEALTH NEXUS')
    expect(store.brand.site_title).toBe('健康宣教平台')
    expect(document.title).toBe('健康宣教平台')

    // setBrand（后台保存路径）同样兜底
    store.setBrand(empty)
    expect(store.brand.name).toBe('Health Nexus')
    expect(store.brand.site_title).toBe('健康宣教平台')
  })
})
