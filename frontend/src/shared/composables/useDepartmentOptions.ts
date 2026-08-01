/**
 * 科室选项加载与树形扁平化 — 统一供 staff 端表单/筛选使用。
 * chat 端的 useDepartments（含选择状态 + "全部科室"语义）保持独立，职责不同。
 */
import { ref } from 'vue'
import { listDepartmentTree } from '@/shared/api/base'
import type { DepartmentTreeNode } from '@/shared'

interface DepartmentOption {
  /** null 表示"全局/全部"哨兵，对应 department_id=null */
  id: number | null
  label: string
}

/** 默认"全局"哨兵，调用方可选注入 */
export const DEPARTMENT_GLOBAL: DepartmentOption = { id: null, label: '— 全局默认 —' }

/**
 * 将树形科室扁平化为带缩进前缀的选项列表，按名称排序。
 * 用于 staff 端表单的 select 控件。
 */
function flattenDepartmentTree(nodes: DepartmentTreeNode[]): DepartmentOption[] {
  const byParent = new Map<number | null, DepartmentTreeNode[]>()
  for (const n of nodes) {
    const arr = byParent.get(n.parent_id) ?? []
    arr.push(n)
    byParent.set(n.parent_id, arr)
  }
  for (const list of byParent.values()) list.sort((a, b) => a.name.localeCompare(b.name, 'zh'))
  const out: DepartmentOption[] = []
  const walk = (parentId: number | null, depth: number) => {
    for (const c of byParent.get(parentId) ?? []) {
      const prefix = depth === 0 ? '' : '　'.repeat(depth) + '└ '
      out.push({ id: c.id, label: prefix + c.name })
      walk(c.id, depth + 1)
    }
  }
  walk(null, 0)
  return out
}

/**
 * 加载科室树并扁平化为带缩进 label 的选项列表（staff 端专用，含未公开科室）。
 * @param sentinel 可选哨兵项，置于列表首位（如 DEPARTMENT_GLOBAL）
 */
export function useDepartmentOptions(sentinel?: DepartmentOption) {
  const options = ref<DepartmentOption[]>(sentinel ? [sentinel] : [])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function load() {
    loading.value = true
    error.value = null
    try {
      const tree = await listDepartmentTree()
      options.value = sentinel ? [sentinel, ...flattenDepartmentTree(tree)] : flattenDepartmentTree(tree)
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      options.value = sentinel ? [sentinel] : []
    } finally {
      loading.value = false
    }
  }

  return { options, loading, error, load }
}

