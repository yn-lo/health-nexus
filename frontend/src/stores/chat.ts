import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as chatApi from '@/shared/api/chat'
import type { Conversation, ConversationUpdateRequest, Message } from '@/shared/types/chat'

// 匿名会话本地持久化：后端匿名上下文存 Redis（12h TTL 自动过期），前端无公开
// 消息拉取端点，故在 localStorage 按 conversation_id 本地缓存消息做刷新回显，
// TTL 与后端 Redis 保持一致的 12h，过期自动清理。
const ANON_MSGS_PREFIX = 'hn_anon_msgs:'
const ANON_MSG_TTL = 12 * 60 * 60 * 1000

function anonKey(conversationId: string): string {
  return ANON_MSGS_PREFIX + conversationId
}

/** 读取匿名会话近期本地消息（超 12h 视为过期并清理） */
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

/** 读取匿名会话索引（已按最近活跃倒序；消息已过期的会话自动剔除，保持与后端 Redis 12h 对齐） */
function loadAnonSessions(): AnonSessionMeta[] {
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

/** 消息单页大小（与后端默认 limit 一致） */
const MESSAGE_PAGE_SIZE = 50
/** 会话列表单页大小（与后端默认 page_size 一致） */
const CONVERSATION_PAGE_SIZE = 20

export const useChatStore = defineStore('chat', () => {
  const conversations = ref<Conversation[]>([])
  const currentConversation = ref<Conversation | null>(null)
  const messages = ref<Message[]>([])
  const loading = ref(false)
  /** 匿名会话索引（本存储存本地，供匿名历史抽屉展示） */
  const anonSessions = ref<AnonSessionMeta[]>([])
  /** 服务端会话总数（用于判断是否还有下一页） */
  const conversationsTotal = ref(0)
  /** 是否还有更早的消息可加载（上一页取满一页即认为还有） */
  const hasMoreMessages = ref(false)
  /** 会话历史加载中：切换会话起为 true，历史到位（或加载失败）后为 false。期间禁止发送 */
  const messagesLoading = ref(false)

  /** 加载匿名会话索引到内存 */
  function loadAnonSessionsList() {
    anonSessions.value = loadAnonSessions()
  }

  // ponytail: 会话加载代次（fetchEpoch）——从"选择会话"开始统一保护详情、科室、消息。
  // 快速切换 A→B 时递增；A 的迟到详情/消息响应因代次过期被丢弃，不会覆盖 B 的界面。
  // 早期实现只在 fetchMessages 开头递增，保护范围太晚：A 的详情仍会覆盖 currentConversation，
  // 且其触发的消息请求会拿到"更新"的代次而被误判为最新响应（P1）。
  let fetchEpoch = 0
  // 本地写入序号：SSE 期间乐观插入的消息会递增此值。
  // 用于拒绝"过期快照"——生成结束后异步回拉整页消息期间用户又发了新消息时，
  // 旧快照整体覆盖会把刚发送的气泡抹掉（epoch 只防止查询之间互相覆盖，不保护本地写入）。
  let localWriteSeq = 0

  /**
   * 开始一次会话加载：递增代次并返回本次操作的代次。
   * 页面在"选择/切换会话"时立即调用（先于详情请求），使详情、科室、消息共享同一代次。
   * 同时即刻隔离旧会话消息并置加载态：切换瞬间旧消息仍可见、且可继续发送会被追加到
   * 旧列表并触发 localWriteSeq，导致新会话历史返回后被整体丢弃（P1）。
   */
  function beginConversationLoad(): number {
    messages.value = []
    hasMoreMessages.value = false
    messagesLoading.value = true
    return ++fetchEpoch
  }

  /** 结束本次会话加载（历史到位或加载失败）；代次过期时不改状态，避免覆盖新会话 */
  function endConversationLoad(epoch: number) {
    if (isCurrentLoad(epoch)) messagesLoading.value = false
  }

  /**
   * 放弃在途会话加载（新建对话/离开会话）：作废代次 + 复位加载态并清空消息列表。
   * 仅递增代次不足以复原加载态（输入栏会被永久禁用），而只清列表又会让在途响应
   * 回来时把已离开的会话历史重新写回。
   */
  function cancelConversationLoad() {
    fetchEpoch++
    messagesLoading.value = false
    messages.value = []
    hasMoreMessages.value = false
  }

  /** 本次代次是否仍是最新（未被后续切换取代）。 */
  function isCurrentLoad(epoch: number): boolean {
    return epoch === fetchEpoch
  }

  /** 获取会话列表（默认不含已归档）。page=1 替换，page>1 追加 */
  async function fetchConversations(page = 1) {
    loading.value = true
    try {
      const res = await chatApi.listConversations({ page, page_size: CONVERSATION_PAGE_SIZE })
      conversations.value = page === 1 ? res.items : [...conversations.value, ...res.items]
      conversationsTotal.value = res.total
    } finally {
      loading.value = false
    }
  }

  /** 加载下一页会话（历史抽屉滚动/点击"加载更多"） */
  async function loadMoreConversations() {
    if (loading.value) return
    if (conversations.value.length >= conversationsTotal.value) return
    const nextPage = Math.floor(conversations.value.length / CONVERSATION_PAGE_SIZE) + 1
    await fetchConversations(nextPage)
  }

  /**
   * 加载单个会话详情（含 archived/last_message_at）。
   * epoch 由调用方在"选择会话"时通过 beginConversationLoad 取得并传入——详情响应迟到
   * （用户已切到别的会话）时不得覆盖 currentConversation，否则界面显示 A 的上下文而提问发往 B（P1）。
   * 未传 epoch（非切换场景，如列表刷新）时不做代次校验。
   */
  async function fetchConversation(conversationId: string, epoch?: number) {
    const conv = await chatApi.getConversation(conversationId)
    // 代次已过期（用户已切换到其他会话）→ 丢弃迟到详情，避免污染当前会话的视图与科室。
    if (epoch !== undefined && !isCurrentLoad(epoch)) return conv
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

  /**
   * 获取会话消息（最新一页）。
   * epoch 由调用方在"选择会话"时取得并传入（与详情同一代次）；未传时自行递增（如生成结束后回拉）。
   * 双重守卫：代次过期（已切换会话）或查询期间用户又发送了消息 → 丢弃过期快照。
   * 同时校验"结果确实属于目标会话"——避免 A 的迟到响应被当作 B 的消息（P1）。
   */
  async function fetchMessages(conversationId: string, epoch?: number) {
    const myEpoch = epoch ?? ++fetchEpoch
    const myWrites = localWriteSeq
    loading.value = true
    try {
      const res = await chatApi.listMessages(conversationId, { limit: MESSAGE_PAGE_SIZE })
      // 已被新请求取代、或查询期间用户又发送了消息（本地列表已更新）→ 丢弃过期快照
      if (!isCurrentLoad(myEpoch) || myWrites !== localWriteSeq) return
      // 防御：响应若不属于当前目标会话，不得覆盖（服务端按 conversation_id 查询，正常不会发生）
      const targetId = currentConversation.value?.id
      if (targetId && res.some((m) => m.conversation_id && m.conversation_id !== targetId)) return
      messages.value = res
      hasMoreMessages.value = res.length >= MESSAGE_PAGE_SIZE
    } finally {
      if (isCurrentLoad(myEpoch)) {
        loading.value = false
        messagesLoading.value = false
      }
    }
  }

  /** 向上加载更早的消息（以当前最早的服务端消息为游标） */
  async function loadEarlierMessages(conversationId: string) {
    if (loading.value || !hasMoreMessages.value) return
    const oldest = messages.value.find((m) => !m.id.startsWith('local-'))
    if (!oldest) return
    const myEpoch = fetchEpoch
    loading.value = true
    try {
      const older = await chatApi.listMessages(conversationId, {
        limit: MESSAGE_PAGE_SIZE,
        before: oldest.id,
      })
      if (!isCurrentLoad(myEpoch)) return // 期间切换/刷新过会话，丢弃
      if (older.length === 0) {
        hasMoreMessages.value = false
        return
      }
      // 后端返回升序；拼接在当前页之前（整体仍为升序）
      messages.value = [...older, ...messages.value]
      hasMoreMessages.value = older.length >= MESSAGE_PAGE_SIZE
    } finally {
      loading.value = false
    }
  }

  /** 添加消息到当前列表（SSE 推送等场景） — 按 id 去重 */
  function addMessage(message: Message) {
    if (messages.value.some((m) => m.id === message.id)) return
    localWriteSeq++
    messages.value.push(message)
  }

  /**
   * 用服务端权威 ID 替换本地乐观 ID。
   * result 事件到达后调用：反馈等后续操作据此定位真实消息，无需按内容猜测。
   */
  function remapMessageId(fromId: string, toId: string) {
    if (!fromId || !toId || fromId === toId) return
    const idx = messages.value.findIndex((m) => m.id === fromId)
    if (idx < 0) return
    const dup = messages.value.findIndex((m) => m.id === toId)
    if (dup >= 0) {
      // 目标消息已在列表中（服务端快照先到）：移除本地占位，避免重复气泡
      messages.value.splice(idx, 1)
      return
    }
    const cur = messages.value[idx]
    if (!cur) return
    messages.value[idx] = { ...cur, id: toId }
  }

  /** 重置整个 store — 登出时调用（Pinia setup store 不支持 $reset()，需手动实现） */
  function $reset() {
    conversations.value = []
    currentConversation.value = null
    messages.value = []
    loading.value = false
    anonSessions.value = []
    conversationsTotal.value = 0
    hasMoreMessages.value = false
    messagesLoading.value = false
    fetchEpoch = 0
    localWriteSeq = 0
  }

  return {
    conversations,
    currentConversation,
    messages,
    loading,
    anonSessions,
    conversationsTotal,
    hasMoreMessages,
    messagesLoading,
    loadAnonSessionsList,
    fetchConversations,
    loadMoreConversations,
    beginConversationLoad,
    endConversationLoad,
    cancelConversationLoad,
    isCurrentLoad,
    fetchConversation,
    updateConversation,
    deleteConversation,
    fetchMessages,
    loadEarlierMessages,
    addMessage,
    remapMessageId,
    $reset,
  }
})
