/**
 * 部门树工具
 * 组装扁平科室数组 → 按 parent_id 分组的子节点 Map（同层按名称排序），
 * 供 DepartmentConfig 的展示列表与父科室候选复用，消除重复的 Map 组装逻辑（jscpd clone）。
 */
import type { DepartmentTreeNode } from '@/shared/types/base'

/**
 * 按 parent_id 组装子节点 Map，同层按名称（中文 localeCompare）排序。
 * 空输入返回空 Map。
 */
export function buildChildMap(nodes: DepartmentTreeNode[]): Map<number | null, DepartmentTreeNode[]> {
  const byParent = new Map<number | null, DepartmentTreeNode[]>()
  for (const n of nodes) {
    const key = n.parent_id
    if (!byParent.has(key)) byParent.set(key, [])
    byParent.get(key)!.push(n)
  }
  for (const list of byParent.values()) list.sort((a, b) => a.name.localeCompare(b.name, 'zh'))
  return byParent
}
