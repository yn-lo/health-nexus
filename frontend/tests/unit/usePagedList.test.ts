/**
 * usePagedList 单元测试
 * 覆盖：初始状态、load 填充、失败回调、onFilterChange 重置分页
 * 背景：4 个配置 CRUD 页（SafetyRule/SensitiveWord/PromptTemplate/AuditLog）的
 *       load() 尾部 + onFilterChange() 重复（jscpd clone），提取为共享 composable
 */
import { describe, it, expect, vi } from 'vitest'
import { usePagedList } from '@/shared/composables/usePagedList'
import { useDsToast } from '@/shared/composables/useDsToast'

const fetcher = () => Promise.resolve({ items: [{ id: 1 }], total: 1 })

describe('usePagedList', () => {
  it('初始状态：空列表、loading=false、page=1、total=0', () => {
    const { items, loading, page, total } = usePagedList({ pageSize: 50, fetcher })
    expect(items.value).toEqual([])
    expect(loading.value).toBe(false)
    expect(page.value).toBe(1)
    expect(total.value).toBe(0)
  })

  it('load() 以 {page, page_size} 调用 fetcher 并填充 items/total', async () => {
    const f = vi.fn(fetcher)
    const { items, total, load } = usePagedList({ pageSize: 50, fetcher: f })
    await load()
    expect(f).toHaveBeenCalledWith({ page: 1, page_size: 50 })
    expect(items.value).toEqual([{ id: 1 }])
    expect(total.value).toBe(1)
  })

  it('load() 失败时调用 onError 且 loading 复位', async () => {
    const onError = vi.fn()
    const { loading, load } = usePagedList({
      pageSize: 50,
      fetcher: () => Promise.reject(new Error('boom')),
      onError,
    })
    await load()
    expect(onError).toHaveBeenCalledTimes(1)
    expect(loading.value).toBe(false)
  })

  it('未传 onError 时默认弹出失败 toast（加载失败）', async () => {
    const { toastState } = useDsToast()
    const { load } = usePagedList({
      pageSize: 50,
      fetcher: () => Promise.reject(new Error('boom')),
    })
    await load()
    expect(toastState.value).toMatchObject({ visible: true, type: 'fail', message: 'boom' })
  })

  it('onFilterChange() 将 page 重置为 1 并重新加载', async () => {
    const f = vi.fn(fetcher)
    const { page, onFilterChange } = usePagedList({ pageSize: 50, fetcher: f })
    page.value = 3
    await onFilterChange()
    expect(page.value).toBe(1)
    expect(f).toHaveBeenCalledWith({ page: 1, page_size: 50 })
  })
})
