import { describe, it, expect } from 'vitest'
import { citedRefs } from '@/shared/utils/citations'
import type { Reference } from '@/shared/types/chat'

function ref(articleId: number, title = `文章${articleId}`): Reference {
  return { chunk_id: `c${articleId}`, article_id: articleId, article_title: title, content: '', score: 0 }
}

describe('citedRefs', () => {
  it('正文无 [资料N] 时返回空', () => {
    expect(citedRefs('没有任何引用的答案', [ref(1), ref(2)])).toEqual([])
  })

  it('按首次引用顺序去重', () => {
    const refs = [ref(1), ref(2)]
    const got = citedRefs('先[资料2]，再[资料1]，又[资料2]', refs)
    expect(got.map((r) => r.article_id)).toEqual([2, 1])
  })

  it('忽略越界与非法编号（模型幻觉）', () => {
    const got = citedRefs('[资料9] [资料x] [资料1]', [ref(1)])
    expect(got.map((r) => r.article_id)).toEqual([1])
  })

  it('历史切片级 references：同一文章的多条切片归并为一条', () => {
    const refs = [ref(1), ref(1), ref(2)]
    const got = citedRefs('[资料1][资料2][资料3]', refs)
    expect(got.map((r) => r.article_id)).toEqual([1, 2])
  })

  it('空格容错', () => {
    expect(citedRefs('[资料 2]', [ref(1), ref(2)]).map((r) => r.article_id)).toEqual([2])
  })

  it('未引用的候选不返回（列表只展示引用集）', () => {
    const refs = [ref(1), ref(2), ref(3)]
    expect(citedRefs('只引用[资料3]', refs).map((r) => r.article_id)).toEqual([3])
  })
})
