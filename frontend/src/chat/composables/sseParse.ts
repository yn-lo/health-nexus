/**
 * SSE 行级解析工具
 * 解析单行 SSE 帧（event: / data: 前缀），供流式解析循环与残留行尾处理复用，
 * 消除 useSSEChat 中两处重复的 if/else 分支（jscpd clone）。
 */

interface SSELine {
  /** event 名称（trim 后）；无 event: 前缀时不存在 */
  event?: string
  /** data 内容（剥离前导空格与 CR 尾缀）；无 data: 前缀时不存在 */
  data?: string
}

/**
 * 解析一行 SSE 文本。
 * - `event: name` → { event: 'name' }（事件名 trim）
 * - `data: payload` → { data: 'payload' }（剥离一个前导空格与 CR 尾缀）
 * - 其他内容 → null（调用方按空行/心跳跳过）
 */
export function parseSSELine(line: string): SSELine | null {
  if (line.startsWith('event:')) {
    return { event: line.slice(6).trim() }
  }
  if (line.startsWith('data:')) {
    return { data: line.slice(5).replace(/^ /, '').replace(/\r$/, '') }
  }
  return null
}
