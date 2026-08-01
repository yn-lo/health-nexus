import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as chatApi from '@/shared/api/chat'
import type { Conversation, ConversationUpdateRequest, Message } from '@/shared/types/chat'

export const useChatStore = defineStore('chat', () => {
  const conversations = ref<Conversation[]>([])
  const currentConversation = ref<Conversation | null>(null)
  const messages = ref<Message[]>([])
  const loading = ref(false)

  // ponytail: 请求 epoch — 防止快速切换会话时旧响应覆盖新响应，折中
  let fetchEpoch = 0

  /** 获取会话列表（默认不含已归档） */
  async function fetchConversations() {
    loading.value = true
    try {
      const res = await chatApi.listConversations()
      conversations.value = res.items
    } finally {
      loading.value = false
    }
  }

  /** 加载单个会话详情（含 archived/last_message_at） */
  async function fetchConversation(conversationId: string) {
    const conv = await chatApi.getConversation(conversationId)
    const idx = conversations.value.findIndex((c) => c.id === conversationId)
    if (idx >= 0) conversations.value[idx] = conv
    else conversations.value.unshift(conv)
    currentConversation.value = conv
    return conv
  }

  /** 修改会话（标题/归档），同步更新本地列表与 currentConversation */
  async function updateConversation(conversationId: string, data: ConversationUpdateRequest) {
    const conv = await chatApi.updateConversation(conversationId, data)
    const idx = conversations.value.findIndex((c) => c.id === conversationId)
    if (idx >= 0) conversations.value[idx] = conv
    if (currentConversation.value?.id === conversationId) currentConversation.value = conv
    return conv
  }

  /** 删除会话 */
  async function deleteConversation(conversationId: string) {
    await chatApi.deleteConversation(conversationId)
    conversations.value = conversations.value.filter((c) => c.id !== conversationId)
    if (currentConversation.value?.id === conversationId) {
      currentConversation.value = null
      messages.value = []
    }
  }

  /** 获取会话消息 — epoch 守卫防止过期响应覆盖 */
  async function fetchMessages(conversationId: string) {
    const myEpoch = ++fetchEpoch
    loading.value = true
    try {
      const res = await chatApi.listMessages(conversationId)
      if (myEpoch !== fetchEpoch) return // 已被新请求取代
      messages.value = res
    } finally {
      if (myEpoch === fetchEpoch) loading.value = false
    }
  }

  /** 添加消息到当前列表（SSE 推送等场景） — 按 id 去重 */
  function addMessage(message: Message) {
    if (messages.value.some((m) => m.id === message.id)) return
    messages.value.push(message)
  }

  /** 重置整个 store — 登出时调用（Pinia setup store 不支持 $reset()，需手动实现） */
  function $reset() {
    conversations.value = []
    currentConversation.value = null
    messages.value = []
    loading.value = false
    fetchEpoch = 0
  }

  return {
    conversations,
    currentConversation,
    messages,
    loading,
    fetchConversations,
    fetchConversation,
    updateConversation,
    deleteConversation,
    fetchMessages,
    addMessage,
    $reset,
  }
})
