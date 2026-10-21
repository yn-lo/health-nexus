import type { Reference } from '@/shared/types/chat'

/**
 * 从答案正文解析实际被引用的文章（按首次引用顺序去重）。
 *
 * - 引用单位 = 文章：正文 `[资料N]` 的 N 指向本轮 references 的第 N 条（后端已按文章归并）。
 * - 未被正文引用的候选文章不进入"参考来源"——避免暗示答案依据了未使用的内容。
 * - 兼容历史会话：旧 references 为切片级，N 仍按切片序号解析后归并到文章。
 * - 越界/非法编号（模型幻觉）忽略。
 */
export function citedRefs(content: string, refs: Reference[] = []): Reference[] {
  const out: Reference[] = []
  const seen = new Set<number>()
  // 正则字面量在每次调用时新建，避免带 g 标志的 lastIndex 状态跨调用残留。
  const re = /\[资料\s*(\d+)\]/g
  let m: RegExpExecArray | null
  while ((m = re.exec(content)) !== null) {
    const ref = refs[Number(m[1]) - 1]
    if (!ref || seen.has(ref.article_id)) continue
    seen.add(ref.article_id)
    out.push(ref)
  }
  return out
}
