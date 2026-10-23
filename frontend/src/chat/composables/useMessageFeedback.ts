import { ref, type Ref } from 'vue'
import { submitMessageFeedback } from '@/shared/api/chat'
import { errmsg } from '@/shared/api/client'
import { useDsToast } from '@/shared/composables'
import type { MessageFeedback } from '@/shared/constants/feedback'
import type { Message } from '@/shared/types/chat'

/** 消息反馈三态：本地乐观更新 + 失败回滚 */
export function useMessageFeedback(isAnon: Ref<boolean>) {
  const feedbackMap = ref<Record<string, string>>({})
  const { showToast, showFailToast } = useDsToast()

  /** 仅服务端已持久化的消息可反馈（本地乐观消息无真实 ID，提交会 404） */
  function canFeedback(msg: Message): boolean {
    return !isAnon.value && !msg.id.startsWith('local-')
  }

  /** 从消息列表恢复服务端反馈，同时保留尚未被服务端回写的本地乐观状态 */
  function syncFromMessages(messages: Message[]) {
    const map: Record<string, string> = {}
    for (const msg of messages) {
      if (msg.feedback) map[msg.id] = msg.feedback
    }
    const oldMap = feedbackMap.value
    for (const [oldId, val] of Object.entries(oldMap)) {
      if (map[oldId]) continue
      if (!oldId.startsWith('local-') || messages.some((m) => m.id === oldId)) {
        map[oldId] = val
      }
    }
    feedbackMap.value = map
  }

  async function submit(msg: Message, value: MessageFeedback) {
    if (feedbackMap.value[msg.id] || !canFeedback(msg)) return
    feedbackMap.value[msg.id] = value
    try {
      await submitMessageFeedback(msg.id, value)
      showToast('感谢您的反馈')
    } catch (e) {
      delete feedbackMap.value[msg.id]
      showFailToast(errmsg(e, '反馈提交失败'))
    }
  }

  return { feedbackMap, canFeedback, syncFromMessages, submit }
}
