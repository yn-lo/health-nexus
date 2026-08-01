/**
 * parseSSELine 单元测试
 * 覆盖：event:/data: 帧前缀解析、trim、前导空格剥离、CR 尾缀剥离、非 SSE 行返回 null
 * 背景：useSSEChat 流式循环与残留行尾两处原为重复 if/else 逻辑（jscpd clone），提取为单一函数
 */
import { describe, it, expect } from 'vitest'
import { parseSSELine } from '@/chat/composables/sseParse'

describe('parseSSELine', () => {
  it('解析 event 行并 trim 事件名', () => {
    expect(parseSSELine('event: token')).toEqual({ event: 'token' })
    expect(parseSSELine('event:  token  ')).toEqual({ event: 'token' })
    expect(parseSSELine('event: conversation')).toEqual({ event: 'conversation' })
  })

  it('解析 data 行，剥离一个分隔空格与 CR 尾缀', () => {
    expect(parseSSELine('data: Hello')).toEqual({ data: 'Hello' })
    // 仅剥离一个前导分隔空格；多余空格属于数据本身（与 useSSEChat 原行为一致）
    expect(parseSSELine('data:  Hello')).toEqual({ data: ' Hello' })
    expect(parseSSELine('data: Hello\r')).toEqual({ data: 'Hello' })
  })

  it('空 event:/data: 前缀行返回空字符串字段（而非 null）', () => {
    expect(parseSSELine('event:')).toEqual({ event: '' })
    expect(parseSSELine('data:')).toEqual({ data: '' })
  })

  it('非 SSE 行返回 null', () => {
    expect(parseSSELine('foo bar')).toBeNull()
    expect(parseSSELine('')).toBeNull()
    expect(parseSSELine('\r')).toBeNull()
  })
})
