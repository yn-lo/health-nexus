/**
 * 品牌配置 store — 全局品牌标识（Logo / 品牌名 / 字标 / 站点标题 / favicon）。
 * 启动时拉取公开接口 `/api/public/brand`（患者端未登录也可用），失败回退内置默认值。
 * Logo 地址为空时回退到打包内置图；站点标题用于覆盖浏览器 tab 标题。
 */
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { configApi } from '@/shared'
import type { BrandConfig } from '@/shared/types/config'
// Logo 兜底图：WebP 192×192（最大显示 80px 容器 × 70%，含 3x 屏余量），2.3KB。
import logoFallback from '@/assets/images/hn_logo.webp'

/** 品牌兜底默认值——与后端 entity.DefaultBrandConfig 对齐；接口不可达时避免空白品牌。 */
const DEFAULT_BRAND: BrandConfig = {
  name: 'Health Nexus',
  wordmark: 'HEALTH NEXUS',
  org_name: '',
  slogan: '',
  site_url: '',
  logo_url: '',
  site_title: '健康宣教平台',
  favicon_url: '',
  updated_at: '',
}

/** 显示层兜底：品牌名称/字标/站点标题为空会渲染成空白品牌与缺失标题，统一回退默认值。 */
function withDisplayFallback(c: BrandConfig): BrandConfig {
  return {
    ...c,
    name: c.name || DEFAULT_BRAND.name,
    wordmark: c.wordmark || DEFAULT_BRAND.wordmark,
    site_title: c.site_title || DEFAULT_BRAND.site_title,
  }
}

export const useBrandStore = defineStore('brand', () => {
  const brand = ref<BrandConfig>({ ...DEFAULT_BRAND })
  /** 标题前缀（按端注入：患者端 / 医护端），空则不前缀 */
  const titlePrefix = ref('')
  let loadPromise: Promise<void> | null = null

  /** Logo 地址：配置为空时回退到内置 logo。 */
  const logoUrl = computed(() => brand.value.logo_url || logoFallback)

  /** 拉取品牌配置并应用运行时标题/favicon。幂等：并发调用复用同一 promise。 */
  function ensureLoaded(): Promise<void> {
    if (!loadPromise) loadPromise = load()
    return loadPromise
  }

  async function load(): Promise<void> {
    try {
      brand.value = withDisplayFallback(await configApi.getPublicBrandConfig())
    } catch {
      // ponytail: 品牌接口不可达时保留默认值，不阻塞页面渲染，折中
    }
    applyDocumentMeta()
  }

  /** 后台保存成功后同步本地，同标签页即时生效（无需刷新）。 */
  function setBrand(next: BrandConfig): void {
    brand.value = withDisplayFallback(next)
    applyDocumentMeta()
  }

  /** 由引导层注入标题前缀（患者端/医护端），随后刷新标题。 */
  function setTitlePrefix(prefix: string): void {
    titlePrefix.value = prefix
    applyDocumentMeta()
  }

  /** 覆盖浏览器标题（favicon 由 <BrandFavicon> 组件 Teleport 注入，避免直接 DOM 操作）。 */
  function applyDocumentMeta(): void {
    const { site_title: siteTitle } = brand.value
    if (siteTitle) {
      document.title = titlePrefix.value ? `${titlePrefix.value} - ${siteTitle}` : siteTitle
    }
  }

  return { brand, logoUrl, ensureLoaded, setBrand, setTitlePrefix }
})
