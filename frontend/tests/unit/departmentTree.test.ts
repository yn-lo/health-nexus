/**
 * buildChildMap 单元测试
 * 覆盖：按 parent_id 分组、同层按名称排序、空输入
 * 背景：DepartmentConfig 的 displayRows / parentCandidates 两处重复的 Map 组装（jscpd clone）
 */
import { describe, it, expect } from 'vitest'
import { buildChildMap } from '@/shared/utils/departmentTree'
import type { DepartmentTreeNode } from '@/shared/types/base'

function node(partial: Partial<DepartmentTreeNode> & Pick<DepartmentTreeNode, 'id' | 'name'>): DepartmentTreeNode {
  return {
    parent_id: null,
    is_public: true,
    is_active: true,
    description: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...partial,
  }
}

describe('buildChildMap', () => {
  it('空输入返回空 Map', () => {
    expect(buildChildMap([]).size).toBe(0)
  })

  it('按 parent_id 分组（null 与具体 id 分离）', () => {
    const nodes = [
      node({ id: 1, name: 'A', parent_id: null }),
      node({ id: 2, name: 'B', parent_id: null }),
      node({ id: 3, name: 'C', parent_id: 1 }),
      node({ id: 4, name: 'D', parent_id: 1 }),
    ]
    const map = buildChildMap(nodes)
    expect(map.get(null)?.map((n) => n.id)).toEqual([1, 2])
    expect(map.get(1)?.map((n) => n.id)).toEqual([3, 4])
    expect(map.has(2)).toBe(false)
  })

  it('同层按 name 中文排序，保证展示稳定', () => {
    const nodes = [
      node({ id: 1, name: '内分泌科', parent_id: null }),
      node({ id: 2, name: '心内科', parent_id: null }),
      node({ id: 3, name: '儿科', parent_id: null }),
    ]
    const map = buildChildMap(nodes)
    expect(map.get(null)?.map((n) => n.name)).toEqual(['儿科', '内分泌科', '心内科'])
  })

  it('不修改输入数组本身（返回新分组）', () => {
    const nodes = [node({ id: 1, name: 'A' }), node({ id: 2, name: 'B' })]
    const original = nodes.map((n) => n.id)
    buildChildMap(nodes)
    expect(nodes.map((n) => n.id)).toEqual(original)
  })
})
