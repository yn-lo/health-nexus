/**
 * format 工具函数单元测试
 * 覆盖：timeAgo 无效输入守卫（null/undefined/空串/非法日期字符串）
 */
import { describe, it, expect } from 'vitest'
import { timeAgo } from '@/shared/utils/format'

describe('timeAgo', () => {
  it('null 返回空字符串（不抛异常）', () => {
    expect(timeAgo(null)).toBe('')
  })

  it('undefined 返回空字符串（不抛异常）', () => {
    expect(timeAgo(undefined)).toBe('')
  })

  it('空字符串返回空字符串', () => {
    expect(timeAgo('')).toBe('')
  })

  it('非法日期字符串返回空字符串', () => {
    expect(timeAgo('not-a-date')).toBe('')
  })

  it('有效 ISO 日期返回非空字符串', () => {
    expect(timeAgo(new Date().toISOString())).toBeTruthy()
  })
})
