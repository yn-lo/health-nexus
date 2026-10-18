/**
 * format 工具函数单元测试
 * 覆盖：timeAgo 无效输入守卫（null/undefined/空串/非法日期字符串）
 */
import { describe, it, expect } from 'vitest'
import { timeAgo, splitSlogan } from '@/shared/utils/format'

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

describe('splitSlogan', () => {
  it('空串回退 fallback', () => {
    expect(splitSlogan('', ['智能健康', '宣教平台'])).toEqual(['智能健康', '宣教平台'])
  })

  it('按字面量 \\n 拆分为多行', () => {
    expect(splitSlogan('智能健康\\n宣教平台', [])).toEqual(['智能健康', '宣教平台'])
  })

  it('去除每行首尾空白', () => {
    expect(splitSlogan(' 智能健康 \\n 宣教平台 ', [])).toEqual(['智能健康', '宣教平台'])
  })

  it('无 \\n 时为单行', () => {
    expect(splitSlogan('健康宣教系统', [])).toEqual(['健康宣教系统'])
  })
})
