import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

export type ChatMode = 'chat' | 'knowledge'

/**
 * useChatMode — 聊天 / 知识库视图模式（同步到 URL query，跨导航可恢复）
 * ChatHome / ChatConversation 的 activeMode + setMode 重复（jscpd clone），提取为共享 composable
 */
export function useChatMode() {
  const router = useRouter()
  const route = useRoute()

  const activeMode = ref<ChatMode>(route.query.mode === 'knowledge' ? 'knowledge' : 'chat')

  /** 切换模式时同步到 URL（replace 避免堆历史，返回时可恢复模式） */
  function setMode(mode: ChatMode) {
    if (activeMode.value === mode) return
    activeMode.value = mode
    router.replace({ query: { ...route.query, mode } })
  }

  return { activeMode, setMode }
}
