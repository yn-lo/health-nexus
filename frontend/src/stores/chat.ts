import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as chatApi from '@/shared/api/chat'
import type { Conversation, ConversationUpdateRequest, Message } from '@/shared/types/chat'

// 匿名会话本地持久化：后端匿名上下文存 Redis（48h TTL 自动过期），前端无公开
// 消息拉取端点，故在 localStorage 按 conversation_id 本地缓存消息做刷新回显，
// TTL 与后端 Redis 保持一致的 48h，过期自动清理。
const ANON_MSGS_PREFIX = 'hn_anon_msgs:'
const ANON_MSG_TTL = 48 * 60 * 60 * 1000

function anonKey(conversationId: string): string {
  return ANON_MSGS_PREFIX + conversationId
}

/** 读取匿名会话近期本地消息（超 48h 视为过期并清理） */
export function loadAnonMessages(conversationId: string): Message[] {
  if (!conversationId) return []
  try {
    const raw = localStorage.getItem(anonKey(conversationId))
    if (!raw) return []
    const data = JSON.parse(raw) as { ts: number; list: Message[] }
    if (Date.now() - data.ts > ANON_MSG_TTL) {
      localStorage.removeItem(anonKey(conversationId))
      return []
    }
    return data.list
  } catch {
    return []
  }
}

/** 持久化匿名会话本地消息（保存后刷新仍可见；存储异常仅丢失本地回显，不影响服务端 Redis 上下文） */
export function saveAnonMessages(conversationId: string, list: Message[]): void {
  if (!conversationId) return
  try {
    localStorage.setItem(anonKey(conversationId), JSON.stringify({ ts: Date.now(), list }))
  } catch {
    /* 存储已满等：忽略 */
  }
}

/** 匿名会话索引项 — 用于匿名历史抽屉（会话无公开查询端点，索引存本地） */
export interface AnonSessionMeta {
  id: string
  title: string
  last_message_at: string
  created_at: string
}

/** 匿名会话索引存储 key */
const ANON_SESSIONS_KEY = 'hn_anon_sessions'
/** 匿名会话索引最多保留条数 */
const ANON_SESSIONS_MAX = 20

function rawAnonSessions(): AnonSessionMeta[] {
  try {
    const raw = localStorage.getItem(ANON_SESSIONS_KEY)
    if (!raw) return []
    return JSON.parse(raw) as AnonSessionMeta[]
  } catch {
    return []
  }
}

function writeAnonSessions(list: AnonSessionMeta[]): void {
  try {
    localStorage.setItem(ANON_SESSIONS_KEY, JSON.stringify(list))
  } catch {
    /* 存储已满等：忽略 */
  }
}

/** 读取匿名会话索引（已按最近活跃倒序；消息已过期的会话自动剔除，保持与后端 Redis 48h 对齐） */
export function loadAnonSessions(): AnonSessionMeta[] {
  return rawAnonSessions().filter((m) => loadAnonMessages(m.id).length > 0)
}

/** 新增/更新一条匿名会话索引（按 id 幂等，写入时移到最前并限长） */
export function upsertAnonSession(meta: AnonSessionMeta): void {
  const list = rawAnonSessions().filter((m) => m.id !== meta.id)
  list.unshift(meta)
  writeAnonSessions(list.slice(0, ANON_SESSIONS_MAX))
}

/** 删除匿名会话及其本地消息缓存（匿名历史抽屉删除） */
export function removeAnonSession(conversationId: string): void {
  writeAnonSessions(rawAnonSessions().filter((m) => m.id !== conversationId))
  localStorage.removeItem(anonKey(conversationId))
}

export const useChatStore = defineStore('chat', () => {
  const conversations = ref<Conversation[]>([])
  const currentConversation = ref<Conversation | null>(null)
  const messages = ref<Message[]>([])
  const loading = ref(false)
  /** 匿名会话索引（本存储存本地，供匿名历史抽屉展示） */
  const anonSessions = ref<AnonSessionMeta[]>([])

  /** 加载匿名会话索引到内存 */
  function loadAnonSessionsList() {
    anonSessions.value = loadAnonSessions()
  }

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
    anonSessions.value = []
    fetchEpoch = 0
  }

  return {
    conversations,
    currentConversation,
    messages,
    loading,
    anonSessions,
    loadAnonSessionsList,
    fetchConversations,
    fetchConversation,
    updateConversation,
    deleteConversation,
    fetchMessages,
    addMessage,
    $reset,
  }
})
