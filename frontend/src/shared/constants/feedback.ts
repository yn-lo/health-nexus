/**
 * 消息反馈三态（宣教效果口径：回答是否解决患者问题）
 * 对齐后端 POST /api/chat/messages/{id}/feedback 取值
 */

/** 反馈类型 */
export type MessageFeedback = 'solved' | 'partial' | 'unsolved'

/** 反馈选项（value + 中文标签）— 患者端按钮与医护端统计共用 */
export const FEEDBACK_OPTIONS: ReadonlyArray<{ value: MessageFeedback; label: string }> = [
  { value: 'solved', label: '解决了' },
  { value: 'partial', label: '部分解决' },
  { value: 'unsolved', label: '未解决' },
]

/** 反馈 value → 中文标签（未知值原样返回） */
export function feedbackLabel(value: string): string {
  return FEEDBACK_OPTIONS.find((o) => o.value === value)?.label ?? value
}
