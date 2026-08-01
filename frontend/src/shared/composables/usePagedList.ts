/**
 * usePagedList — 分页列表加载
 * SafetyRuleConfig / SensitiveWordConfig / PromptTemplateConfig / AuditLogConfig
 * 的 load() 尾部 + onFilterChange() 重复（jscpd clone），提取为共享 composable
 */
import { ref } from 'vue'
import { useDsToast } from './useDsToast'
import { errmsg } from '@/shared/api/client'

interface PagedResult<T> {
  items: T[]
  total: number
}

interface UsePagedListOptions<T> {
  /** 每页条数 */
  pageSize: number
  /** 实际取数逻辑（接收分页参数，返回 items/total） */
  fetcher: (params: { page: number; page_size: number }) => Promise<PagedResult<T>>
  /** 加载失败回调；缺省时自动弹出失败 toast（'加载失败'） */
  onError?: (e: unknown) => void
}

export function usePagedList<T>(options: UsePagedListOptions<T>) {
  const items = ref<T[]>([])
  const loading = ref(false)
  const page = ref(1)
  const total = ref(0)
  const { pageSize } = options
  const { showFailToast } = useDsToast()
  const handleError = options.onError ?? ((e: unknown) => showFailToast(errmsg(e, '加载失败')))

  async function load() {
    loading.value = true
    try {
      const res = await options.fetcher({ page: page.value, page_size: pageSize })
      items.value = res.items
      total.value = res.total
    } catch (e) {
      handleError(e)
    } finally {
      loading.value = false
    }
  }

  /** 筛选条件变化：回到第一页并刷新 */
  function onFilterChange() {
    page.value = 1
    load()
  }

  return { items, loading, page, pageSize, total, load, onFilterChange }
}
