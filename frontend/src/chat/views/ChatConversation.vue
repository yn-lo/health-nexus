<script setup lang="ts">
/**
 * ChatConversation 对话页 - AI-Native UI 风格（重构后）
 *
 * 视觉重塑：
 *   - 顶部统一 ChatHeader（frosted variant + 返回 + 标题 + 历史）
 *   - 用户消息：品牌色实心气泡 + 白色文本（右对齐，更强的对话对比）
 *   - AI 消息：灰底气泡 + 名称/时间层级 + 编号引用卡 + 反馈栏（左对齐）
 *   - 消息按角色分组：连续同角色收拢间距，跨角色留白更大
 *   - 15px/24px 阅读字号 + 分组呼吸感（message gap 提升至 20px）
 *   - 3 点输入指示器（pulse）+ 流式光标
 * 保留功能：useSSEChat、useDepartments、ChatHistoryDrawer、点踩原因
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Copy,
  Sparkles,
  ThumbsDown,
  ThumbsUp,
} from '@lucide/vue'
import { useDepartments } from '@/chat/composables/useDepartments'
import { useSSEChat } from '@/chat/composables/useSSEChat'
import { useChatStore, loadAnonMessages, saveAnonMessages, upsertAnonSession, type AnonSessionMeta } from '@/stores/chat'
import { DisclaimerFooter, DsActionSheet } from '@/shared/components'
import { useDsToast, useDsDialog } from '@/shared/composables'
import ChatHeader from '@/chat/components/ChatHeader.vue'
import ChatInputBar from '@/chat/components/ChatInputBar.vue'
import ChatHistoryDrawer from '@/chat/components/ChatHistoryDrawer.vue'
import DeptPickerPopup from '@/chat/components/DeptPickerPopup.vue'
import KnowledgeList from '@/chat/views/KnowledgeList.vue'
import MarkdownIt from 'markdown-it'
import { sanitizeHtml } from '@/shared/utils/sanitize-html'
import { submitMessageFeedback } from '@/shared/api/chat'
import { errmsg, getAccessToken } from '@/shared/api/client'
import type { Message, Reference } from '@/shared/types/chat'

const router = useRouter()
const route = useRoute()
const chatStore = useChatStore()
const { showToast, showFailToast } = useDsToast()
const { showDialog } = useDsDialog()

const md = new MarkdownIt({ html: false, breaks: true, linkify: true })

/** 渲染 markdown 为 HTML（经白名单消毒，防 XSS） */
function renderMd(text: string): string {
  return sanitizeHtml(md.render(text))
}

/** 消息时间：今天显示 HH:mm，否则显示 MM-DD HH:mm */
function formatTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  const now = new Date()
  const sameDay = d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() && d.getDate() === now.getDate()
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  if (sameDay) return hm
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${hm}`
}

/** 与下一条消息同角色 → 收拢间距（分组呼吸感） */
function isGroupedWithNext(idx: number): boolean {
  const next = chatStore.messages[idx + 1]
  const cur = chatStore.messages[idx]
  if (!cur || !next) return false
  return cur.role === next.role
}

const inputBarRef = ref<InstanceType<typeof ChatInputBar> | null>(null)
const messageListRef = ref<HTMLDivElement>()
const inputBarHeight = ref(120)
let inputBarObserver: ResizeObserver | null = null
const showHistory = ref(false)
const showDeptPicker = ref(false)
const showDownReasonSheet = ref(false)
const pendingDownMessageId = ref<string | null>(null)
const feedbackMap = ref<Record<string, string>>({})

const activeMode = ref<'chat' | 'knowledge'>(route.query.mode === 'knowledge' ? 'knowledge' : 'chat')

/** 切换模式时同步到 URL（replace 避免堆历史，返回时可恢复模式） */
function setMode(mode: 'chat' | 'knowledge') {
  if (activeMode.value === mode) return
  activeMode.value = mode
  router.replace({ query: { ...route.query, mode } })
}
const knowledgeRef = ref<InstanceType<typeof KnowledgeList> | null>(null)

const initialDepartmentId = Number(route.query.department ?? 0)
const { departments, selectedDepartmentId, activeDepartment, selectDepartment } = useDepartments({ initialDepartmentId })

// SSE: sseOptions 为可变对象，route 变化时更新 conversationId
const sseOptions = { conversationId: (route.params.id as string) ?? '', selectedDeptId: selectedDepartmentId.value }
const { isStreaming, currentContent, references, crisis, error, aborted, conversationId: sseConversationId, sendQuestion, abort } = useSSEChat(sseOptions)

const conversationId = computed(() => (route.params.id as string) ?? '')
const isThinking = computed(() => isStreaming.value && !currentContent.value)
// 匿名用户（无 access token）：走 /api/public/chat/stream，上下文存服务端 Redis 12h，
// 前端无公开消息拉取/反馈端点——消息本地回显（localStorage），隐藏反馈入口，避免 401 与无效反馈。
const isAnon = !getAccessToken()

/** 匿名会话：将当前消息列表持久化到 localStorage（key 绑定会话 id），刷新后回显 */
function persistAnonMessages() {
  if (!isAnon) return
  const convId = sseConversationId.value || conversationId.value
  if (convId) {
    saveAnonMessages(convId, chatStore.messages)
    upsertAnonSession(buildAnonSessionMeta(convId, chatStore.messages))
  }
}

/** 由消息列表构建匿名会话索引元信息（标题=首条用户消息，活跃时间=最后一条消息时间） */
function buildAnonSessionMeta(convId: string, list: Message[]): AnonSessionMeta {
  const firstUser = list.find((m) => m.role === 'user')
  const last = list[list.length - 1]
  return {
    id: convId,
    title: truncateTitle(firstUser?.content ?? ''),
    created_at: list[0]?.created_at ?? new Date().toISOString(),
    last_message_at: last?.created_at ?? new Date().toISOString(),
  }
}

/** 截断会话标题到 24 字符（对齐后端 truncateTitle 语义） */
function truncateTitle(title: string): string {
  const t = title.trim().replace(/\s+/g, ' ')
  if (!t) return '新对话'
  return t.length > 24 ? t.slice(0, 24) + '…' : t
}

const downReasonActions: { name: string }[] = [
  { name: '回答不准确' },
  { name: '回答不完整' },
  { name: '回答不相关' },
  { name: '内容不安全' },
  { name: '其他' },
]

function openHistory() {
  showHistory.value = true
}

function onHistorySelect(id: string) {
  router.push({ name: 'chat-conversation', params: { id } })
  showHistory.value = false
}

/** 新建对话：回到 /chat 首页（真正的全新对话入口）；流式中先中止并复位当前会话 */
function startNewChat() {
  if (isStreaming.value) {
    abort()
    currentContent.value = ''
  }
  showHistory.value = false
  sseConversationId.value = ''
  sseOptions.conversationId = ''
  sseOptions.selectedDeptId = selectedDepartmentId.value
  chatStore.messages = []
  router.replace({ name: 'chat-home' })
}

function openDeptPicker() {
  // 会话已开始（有会话 ID）后禁止切换科室：科室范围随会话锁定，保持多轮上下文一致性。
  // 后端同样拦截（CHAT_DEPT_LOCKED），此处前端先拦截，避免无效弹窗。
  if (conversationId.value || sseConversationId.value) {
    showFailToast('会话中禁止切换知识库')
    return
  }
  showDeptPicker.value = true
}

function onDeptSelect(id: number) {
  selectDepartment(id)
  sseOptions.selectedDeptId = id
}

/** 距底部小于该阈值视为"跟随中"，用户上滑回看时不强制拉回 */
const AUTO_FOLLOW_BOTTOM_PX = 80

function isNearBottom(): boolean {
  const el = messageListRef.value
  if (!el) return true
  return el.scrollHeight - el.scrollTop - el.clientHeight < AUTO_FOLLOW_BOTTOM_PX
}

/** force=true 用于用户自身操作（发送/切换会话），否则仅在已接近底部时跟随 */
function scrollToBottom(force = false) {
  if (!force && !isNearBottom()) return
  nextTick(() => {
    if (messageListRef.value) {
      messageListRef.value.scrollTop = messageListRef.value.scrollHeight
    }
  })
}

function syncFeedbackFromMessages() {
  const map: Record<string, string> = {}
  for (const msg of chatStore.messages) {
    if (msg.feedback) map[msg.id] = msg.feedback
  }
  const oldMap = feedbackMap.value
  for (const [oldId, val] of Object.entries(oldMap)) {
    if (map[oldId]) continue
    if (!oldId.startsWith('local-')) {
      map[oldId] = val
      continue
    }
    const stillInStore = chatStore.messages.some((m) => m.id === oldId)
    if (stillInStore) map[oldId] = val
  }
  feedbackMap.value = map
}

function sendMessage(text: string) {
  if (!text || isStreaming.value) return

  chatStore.addMessage({
    id: `local-${crypto.randomUUID()}`,
    conversation_id: conversationId.value,
    role: 'user',
    content: text,
    result_code: null,
    references: [],
    created_at: new Date().toISOString(),
  })
  scrollToBottom(true)

  // SSE 流式发送；streaming 结束由 watch(isStreaming) 收尾
  sendQuestion(text)
}

async function copyMessage(content: string) {
  if (!navigator.clipboard) {
    showFailToast('当前浏览器不支持复制')
    return
  }
  try {
    await navigator.clipboard.writeText(content)
    showToast('已复制')
  } catch {
    showFailToast('复制失败')
  }
}

/** 按 article_id 去重引用，只保留每篇文章第一条 */
function dedupeRefs(refs: Reference[]): Reference[] {
  const seen = new Map<number, Reference>()
  for (const ref of refs) {
    if (!seen.has(ref.article_id)) {
      seen.set(ref.article_id, ref)
    }
  }
  return Array.from(seen.values())
}

/** 跳转到文章详情页 */
function goToArticle(articleId: number) {
  router.push({ name: 'wiki-article', params: { id: articleId } })
}

async function resolveServerMsgId(msg: Message): Promise<string | null> {
  if (!msg.id.startsWith('local-')) return msg.id
  const convId = sseConversationId.value || conversationId.value
  if (!convId) return null
  try {
    await chatStore.fetchMessages(convId)
    syncFeedbackFromMessages()
    const serverMsg = chatStore.messages.find(
      (m) => !m.id.startsWith('local-') && m.role === msg.role && m.content === msg.content,
    )
    return serverMsg?.id ?? null
  } catch {
    return null
  }
}

async function onThumbsUp(msg: Message) {
  if (feedbackMap.value[msg.id]) return
  feedbackMap.value[msg.id] = 'up'
  const serverId = await resolveServerMsgId(msg)
  if (serverId && serverId !== msg.id) {
    feedbackMap.value[serverId] = 'up'
    delete feedbackMap.value[msg.id]
  }
  try {
    await submitMessageFeedback(serverId ?? msg.id, 'up')
    showToast('感谢您的反馈')
  } catch (e) {
    delete feedbackMap.value[msg.id]
    if (serverId && serverId !== msg.id) delete feedbackMap.value[serverId]
    showFailToast(errmsg(e, '反馈提交失败'))
  }
}

function onThumbsDown(msg: Message) {
  if (feedbackMap.value[msg.id]) return
  pendingDownMessageId.value = msg.id
  showDownReasonSheet.value = true
}

async function onDownReasonSelect(_action: { name: string }) {
  showDownReasonSheet.value = false
  const msgId = pendingDownMessageId.value
  if (msgId === null) return
  pendingDownMessageId.value = null
  feedbackMap.value[msgId] = 'down'
  const msg = chatStore.messages.find((m) => m.id === msgId)
  const serverId = msg ? await resolveServerMsgId(msg) : null
  if (serverId && serverId !== msgId) {
    feedbackMap.value[serverId] = 'down'
    delete feedbackMap.value[msgId]
  }
  try {
    await submitMessageFeedback(serverId ?? msgId, 'down')
    showToast('感谢您的反馈')
  } catch (e) {
    delete feedbackMap.value[msgId]
    if (serverId && serverId !== msgId) delete feedbackMap.value[serverId]
    showFailToast(errmsg(e, '反馈提交失败'))
  }
}

// Route 变化：中止旧流、更新 conversationId、重新加载消息
watch(conversationId, (newId) => {
  // SSE conversation 事件触发的路由同步：sseConversationId 已是最新，跳过避免中断流
  if (newId === sseConversationId.value) return
  if (isStreaming.value) {
    abort()
    currentContent.value = ''
  }
  sseConversationId.value = newId
  sseOptions.conversationId = newId
  if (newId) {
    if (isAnon) {
      // 匿名：无公开拉取端点，从 localStorage 回显本地缓存（服务端 Redis 上下文此时不展示给用户）
      chatStore.messages = loadAnonMessages(newId)
      scrollToBottom()
      return
    }
    void chatStore.fetchMessages(newId).then(() => {
      syncFeedbackFromMessages()
      scrollToBottom()
    }).catch(() => {
      showFailToast('加载消息失败')
    })
  }
})

// 后端 conversation 事件下发新会话 ID → 更新路由（URL 反映真实会话，后续消息自动携带）
watch(sseConversationId, (id) => {
  if (id && id !== conversationId.value) {
    router.replace({ name: 'chat-conversation', params: { id } })
  }
})

// SSE 结束：将累积内容固化为 AI 消息
watch(isStreaming, (streaming, prev) => {
  if (!prev || streaming) return
  if (error.value) {
    showFailToast(error.value)
    // error 事件时仍将已累积的部分内容固化为消息，避免用户已看到的流式片段丢失
    if (currentContent.value) {
      chatStore.addMessage({
        id: `local-assistant-${crypto.randomUUID()}`,
        conversation_id: conversationId.value,
        role: 'assistant',
        content: currentContent.value,
        result_code: 'INTERCEPTED',
        references: references.value,
        created_at: new Date().toISOString(),
      })
    }
    persistAnonMessages()
    return
  }
  if (crisis.value) {
    showDialog({
      title: '心理援助',
      message: crisis.value.answer,
      confirmButtonText: '我已了解',
    })
    chatStore.addMessage({
      id: `local-assistant-${crypto.randomUUID()}`,
      conversation_id: conversationId.value,
      role: 'assistant',
      content: crisis.value.answer,
      result_code: 'CRISIS',
      references: [],
      created_at: new Date().toISOString(),
    })
    scrollToBottom()
    persistAnonMessages()
    return
  }
  if (currentContent.value) {
    chatStore.addMessage({
      id: `local-assistant-${crypto.randomUUID()}`,
      conversation_id: conversationId.value,
      role: 'assistant',
      content: currentContent.value,
      result_code: aborted.value ? 'INTERCEPTED' : 'ANSWERED',
      references: references.value,
      created_at: new Date().toISOString(),
    })
  }
  scrollToBottom()
  persistAnonMessages()
  const convId = sseConversationId.value || conversationId.value
  if (convId && !isAnon) {
    chatStore.fetchMessages(convId).then(() => {
      syncFeedbackFromMessages()
      scrollToBottom()
    }).catch(() => {})
  }
})

// 流式输出时自动滚动
watch(currentContent, scrollToBottom)

onMounted(async () => {
  const barEl = inputBarRef.value?.$el as HTMLElement | undefined
  if (barEl) {
    inputBarHeight.value = barEl.offsetHeight + 16
    inputBarObserver = new ResizeObserver(() => {
      inputBarHeight.value = barEl.offsetHeight + 16
    })
    inputBarObserver.observe(barEl)
  }

  const initialQuestion = route.query.q as string | undefined
  if (initialQuestion) {
    if (!conversationId.value) {
      chatStore.messages = []
    }
    await router.replace({ name: 'chat-conversation', params: { id: conversationId.value || undefined } })
    sendMessage(initialQuestion)
    return
  }

  if (!conversationId.value) return
  if (isAnon) {
    // 匿名：无公开拉取端点，从 localStorage 回显本地缓存
    chatStore.messages = loadAnonMessages(conversationId.value)
    scrollToBottom(true)
    return
  }
  try {
    await chatStore.fetchMessages(conversationId.value)
    syncFeedbackFromMessages()
    scrollToBottom(true)
  } catch {
    showFailToast('加载消息失败')
  }
})

onUnmounted(() => {
  inputBarObserver?.disconnect()
})
</script>

<template>
  <div class="chat-conversation flex flex-col h-[100dvh] bg-[var(--bg-base-secondary)]">
    <!-- 顶部栏 - AI-Native frosted -->
    <ChatHeader variant="transparent" @open-history="openHistory">
      <template #center>
        <div class="ds-fab-segment ds-fab-segment--neutral">
          <button
            type="button"
            class="ds-fab-segment__btn"
            :class="{ 'ds-fab-segment__btn--active': activeMode === 'chat' }"
            @click="setMode('chat')"
          >
            聊天
          </button>
          <button
            type="button"
            class="ds-fab-segment__btn"
            :class="{ 'ds-fab-segment__btn--active': activeMode === 'knowledge' }"
            @click="setMode('knowledge')"
          >
            知识库
          </button>
        </div>
      </template>
    </ChatHeader>

    <!-- 消息列表 - AI-Native：分组留白 + 15px 阅读字号 -->
    <main v-if="activeMode === 'chat'" ref="messageListRef" class="flex-1 overflow-y-auto px-[var(--spacer-16)] py-[var(--spacer-20)] no-scrollbar" :style="{ paddingBottom: `${inputBarHeight}px` }">
      <div class="flex flex-col">
        <template v-for="(msg, idx) in chatStore.messages" :key="msg.id">
          <!-- 用户消息 - 品牌浅色气泡 + 时间（右对齐） -->
          <div
            v-if="msg.role === 'user'"
            class="chat-row chat-row--user"
            :class="{ 'chat-row--grouped': isGroupedWithNext(idx) }"
          >
            <div class="chat-row__content chat-row__content--user">
              <div class="ds-bubble ds-bubble--user">
                <div class="m-0 break-words markdown-content" v-html="renderMd(msg.content)"></div>
              </div>
              <span class="chat-time">{{ formatTime(msg.created_at) }}</span>
            </div>
          </div>

          <!-- AI 消息 - 灰底气泡 + 名称层级 + 引用 + 反馈 -->
          <div
            v-else-if="msg.role === 'assistant'"
            class="chat-row chat-row--ai"
            :class="{ 'chat-row--grouped': isGroupedWithNext(idx) }"
          >
            <div class="ds-avatar ds-avatar--brand mt-[var(--spacer-4)]">
              <Sparkles :size="14" />
            </div>
            <div class="flex-1 min-w-0 chat-row__content">
              <div class="chat-assistant-name">
                <span class="font-medium text-text">健康助手</span>
                <span class="chat-time">{{ formatTime(msg.created_at) }}</span>
              </div>
              <div class="ds-bubble ds-bubble--ai">
                <div class="m-0 break-words markdown-content" v-html="renderMd(msg.content)"></div>
              </div>

              <!-- 引用来源 -->
              <div v-if="msg.references?.length" class="mt-[var(--spacer-12)]">
                <p class="text-body-xs text-text-tertiary m-0 mb-[var(--spacer-6)]">参考来源</p>
                <div class="flex flex-col gap-[var(--spacer-6)]">
                  <div
                    v-for="(ref, refIdx) in dedupeRefs(msg.references)"
                    :key="ref.article_id"
                    class="ref-card group flex items-center gap-[var(--spacer-10)] rounded-[var(--radius-12)] bg-[var(--bg-overlay-l1)] px-[var(--spacer-12)] py-[var(--spacer-8)] ring-1 ring-[var(--border-neutral-l1)] transition-all active:scale-[0.98]"
                    @click="goToArticle(ref.article_id)"
                  >
                    <span class="ref-card__idx">{{ refIdx + 1 }}</span>
                    <p class="flex-1 min-w-0 text-body-sm text-text-secondary m-0 leading-snug line-clamp-2">
                      {{ ref.article_title }}
                    </p>
                  </div>
                </div>
              </div>

              <!-- 反馈栏（匿名用户无服务端持久化，仅保留复制） -->
              <div class="feedback-bar flex items-center mt-[var(--spacer-10)]">
                <template v-if="!isAnon">
                  <button
                    class="feedback-btn flex items-center justify-center"
                    :class="feedbackMap[msg.id] === 'up' ? 'text-icon-brand' : 'text-icon-tertiary'"
                    :aria-pressed="feedbackMap[msg.id] === 'up'"
                    aria-label="有帮助"
                    @click="onThumbsUp(msg)"
                  >
                    <ThumbsUp :size="15" />
                  </button>
                  <button
                    class="feedback-btn flex items-center justify-center"
                    :class="feedbackMap[msg.id] === 'down' ? 'text-icon-brand' : 'text-icon-tertiary'"
                    :aria-pressed="feedbackMap[msg.id] === 'down'"
                    aria-label="无帮助"
                    @click="onThumbsDown(msg)"
                  >
                    <ThumbsDown :size="16" />
                  </button>
                </template>
                <button
                  class="feedback-btn flex items-center justify-center text-icon-tertiary"
                  aria-label="复制内容"
                  @click="copyMessage(msg.content)"
                >
                  <Copy :size="15" />
                </button>
              </div>
            </div>
          </div>
        </template>

        <!-- 思考中指示器 - 3-dot pulse + 名称 -->
        <div v-if="isThinking" class="chat-row chat-row--ai">
          <div class="ds-avatar ds-avatar--brand mt-[var(--spacer-4)]">
            <Sparkles :size="14" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="chat-assistant-name">
              <span class="font-medium text-text">健康助手</span>
              <span class="chat-time">正在思考</span>
            </div>
            <div class="ds-bubble ds-bubble--ai">
              <div class="flex items-center gap-[var(--spacer-6)]">
                <span class="thinking-dot" />
                <span class="thinking-dot" />
                <span class="thinking-dot" />
                <span class="ml-[var(--spacer-4)] text-body-xs text-text-tertiary">正在思考...</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 流式输出中的 AI 消息 -->
        <div v-if="isStreaming && currentContent" class="chat-row chat-row--ai">
          <div class="ds-avatar ds-avatar--brand mt-[var(--spacer-4)]">
            <Sparkles :size="14" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="chat-assistant-name">
              <span class="font-medium text-text">健康助手</span>
              <span class="chat-time">正在生成</span>
            </div>
            <div class="ds-bubble ds-bubble--ai">
              <div class="m-0 break-words markdown-content" v-html="renderMd(currentContent) + '<span class=\'streaming-cursor\'></span>'"></div>
            </div>
            <!-- 流式中的引用来源 -->
            <div v-if="references.length" class="mt-[var(--spacer-12)]">
              <p class="text-body-xs text-text-tertiary m-0 mb-[var(--spacer-6)]">参考来源</p>
              <div class="flex flex-col gap-[var(--spacer-6)]">
                <div
                  v-for="(ref, refIdx) in dedupeRefs(references)"
                  :key="ref.article_id"
                  class="ref-card group flex items-center gap-[var(--spacer-10)] rounded-[var(--radius-12)] bg-[var(--bg-overlay-l1)] px-[var(--spacer-12)] py-[var(--spacer-8)] ring-1 ring-[var(--border-neutral-l1)] transition-all active:scale-[0.98]"
                  @click="goToArticle(ref.article_id)"
                >
                  <span class="ref-card__idx">{{ refIdx + 1 }}</span>
                  <p class="flex-1 min-w-0 text-body-sm text-text-secondary m-0 leading-snug line-clamp-2">
                    {{ ref.article_title }}
                  </p>
                </div>
              </div>
            </div>
            <!-- 流式中仅保留复制按钮 -->
            <div class="feedback-bar flex items-center mt-[var(--spacer-10)]">
              <button
                class="feedback-btn flex items-center justify-center text-icon-tertiary"
                aria-label="复制内容"
                @click="copyMessage(currentContent)"
              >
                <Copy :size="15" />
              </button>
            </div>
          </div>
        </div>

        <!-- 免责声明 -->
        <DisclaimerFooter />
      </div>
    </main>

    <!-- 知识库模式 -->
    <KnowledgeList v-if="activeMode === 'knowledge'" ref="knowledgeRef" embedded />

    <!-- 底部输入栏 - AI-Native：fixed bottom -->
    <ChatInputBar
      v-if="activeMode === 'chat'"
      ref="inputBarRef"
      class="chat-conversation-input-bar"
      :department-name="activeDepartment.name"
      :loading="isStreaming"
      placeholder="输入您的健康问题..."
      @send="sendMessage"
      @stop="abort"
      @open-dept-picker="openDeptPicker"
    />

    <!-- 历史抽屉 -->
    <ChatHistoryDrawer
      v-model:visible="showHistory"
      @select="onHistorySelect"
      @new-chat="startNewChat"
    />

    <!-- 科室选择弹窗 -->
    <DeptPickerPopup
      v-model:show="showDeptPicker"
      :departments="departments"
      :selected-id="selectedDepartmentId"
      @select="onDeptSelect"
    />

    <!-- 点踩原因选择 -->
    <DsActionSheet
      :show="showDownReasonSheet"
      :actions="downReasonActions"
      cancel-text="取消"
      close-on-click-action
      @select="onDownReasonSelect"
      @update:show="showDownReasonSheet = $event"
    />
  </div>
</template>

<style scoped ponytail:allow-scoped-css 组件级样式覆盖，折中>
/* ── AI-Native 3-dot pulse（模板默认 8px） ─────────────── */
.thinking-dot {
  width: var(--typing-dot-size);
  height: var(--typing-dot-size);
  border-radius: var(--radius-full);
  background: var(--ai-accent);
  animation: thinking-pulse 1.4s ease-in-out infinite;
}
.thinking-dot:nth-child(2) {
  animation-delay: 0.16s;
}
.thinking-dot:nth-child(3) {
  animation-delay: 0.32s;
}

@keyframes thinking-pulse {
  0%, 80%, 100% {
    transform: scale(0.6);
    opacity: 0.5;
  }
  40% {
    transform: scale(1);
    opacity: 1;
  }
}

/* ── AI-Native 流式光标 ───────────────────────────────── */
.streaming-cursor {
  display: inline-block;
  width: 2px;
  height: 14px;
  background: var(--ai-accent);
  vertical-align: text-bottom;
  animation: cursor-blink 1s step-end infinite;
}

@keyframes cursor-blink {
  0%, 50% { opacity: 1; }
  51%, 100% { opacity: 0; }
}

/* ── 消息行：跨角色分组留白 + 同角色收拢 ───────────────── */
.ai-message,
.chat-row {
  animation: message-fade-in 200ms var(--micro-ease) both;
}

.chat-row {
  display: flex;
  align-items: flex-start;
  gap: var(--spacer-10);
  margin-bottom: var(--message-gap);
}
.chat-row:last-child {
  margin-bottom: 0;
}
/* 与下一条同角色 → 收拢间距（分组呼吸感） */
.chat-row--grouped {
  margin-bottom: var(--spacer-10);
}

/* 用户消息：右对齐，气泡后跟时间 */
.chat-row--user {
  justify-content: flex-end;
}
.chat-row__content--user {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: var(--spacer-4);
  /* 用户气泡宽度预算：落在整行上而非随内容塌缩的列上，使 88% 真正生效 */
  width: auto;
  flex: 0 1 auto;
  max-width: 88%;
}

/* 气泡填满内容列，让 88% 的限宽由列承接 */
.chat-row__content--user .ds-bubble {
  max-width: 100%;
}

/* AI 消息名称 + 时间层级 */
.chat-assistant-name {
  display: flex;
  align-items: baseline;
  gap: var(--spacer-6);
  margin-bottom: var(--spacer-4);
}
.chat-assistant-name .chat-time {
  font-size: var(--body-xs-font-size);
  color: var(--text-tertiary);
}

.chat-time {
  font-size: var(--body-xs-font-size);
  color: var(--text-tertiary);
  line-height: 1.2;
}

@keyframes message-fade-in {
  from {
    opacity: 0;
    transform: translateY(4px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* ── Micro-interactions：反馈栏 ─────────────────────────── */
.feedback-bar {
  gap: var(--spacer-4);
}

/* ── Micro-interactions：反馈按钮 hover/press ───────────── */
.feedback-btn {
  width: 36px;
  height: 36px;
  border-radius: var(--radius-full);
  transition: transform var(--micro-duration) var(--micro-ease),
              background-color var(--micro-duration) var(--micro-ease),
              color var(--micro-duration) var(--micro-ease);
}
.feedback-btn:hover {
  background: var(--bg-overlay-l1);
}
.feedback-btn:active {
  transform: scale(var(--press-scale));
}

/* ── 引用来源卡片：编号徽章 + 触感按压 ─────────────────── */
.ref-card__idx {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  border-radius: var(--radius-8);
  background: var(--bg-brand-light);
  color: var(--text-brand);
  font-size: var(--body-xs-font-size);
  font-weight: 600;
}

/* ── 胶囊输入栏 focus 光晕 ─────────────────────────────── */
.input-capsule {
  transition: border-color var(--micro-duration) var(--micro-ease),
              box-shadow var(--micro-duration) var(--micro-ease);
}
.input-capsule:focus-within {
  border-color: var(--border-brand);
  box-shadow: 0 0 0 var(--focus-ring-width) var(--hero-glow-color-soft);
}

/* ── 固定底部输入栏 ─────────────────────────────────────── */
.chat-conversation-input-bar {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  z-index: 20;
}

/* ── A11y：减弱动效偏好 ─────────────────────────────────── */
@media (prefers-reduced-motion: reduce) {
  .thinking-dot,
  .streaming-cursor,
  .ai-message,
  .chat-row {
    animation: none;
  }
  .feedback-btn {
    transition: none;
  }
  .feedback-btn:active {
    transform: none;
  }
}
/* ── Markdown 内容：紧凑可读排版 ────────────────────────────
 * 关键修复：覆盖 .ds-bubble 继承的 white-space: pre-wrap。
 * markdown-it 输出的 HTML 在块级标签间存在源码换行，pre-wrap 会
 * 把这些换行渲染成可见空行，导致嵌套列表出现过大间距。
 * ─────────────────────────────────────────────────────────── */
/* 阅读字号：用户/AI 气泡统一 15px/24px（比默认 body-sm 更舒适） */
.chat-row .ds-bubble {
  font-size: 15px;
  line-height: 24px;
}
.chat-row .markdown-content {
  font-size: 15px;
  line-height: 24px;
}
.markdown-content {
  white-space: normal;
}
.markdown-content :deep(p) {
  margin: 0 0 0.4em;
}
.markdown-content :deep(p:last-child) {
  margin-bottom: 0;
}
.markdown-content :deep(p + p) {
  margin-top: 0.2em;
}
.markdown-content :deep(ol),
.markdown-content :deep(ul) {
  margin: 0.3em 0;
  padding-left: 1.3em;
}
.markdown-content :deep(ol:last-child),
.markdown-content :deep(ul:last-child) {
  margin-bottom: 0;
}
.markdown-content :deep(li) {
  margin: 0.15em 0;
}
.markdown-content :deep(li > p) {
  margin: 0 0 0.25em;
}
.markdown-content :deep(li > p:last-child) {
  margin-bottom: 0;
}
/* 嵌套列表收紧（防御性：prompt 已禁止嵌套，此处兜底旧数据） */
.markdown-content :deep(li ul),
.markdown-content :deep(li ol) {
  margin: 0.2em 0;
  padding-left: 0.8em;
}
/* 二层以上嵌套几乎不缩进，避免窄屏溢出 */
.markdown-content :deep(li li ul),
.markdown-content :deep(li li ol) {
  padding-left: 0.4em;
}
.markdown-content :deep(strong) {
  font-weight: 600;
}
.markdown-content :deep(a) {
  color: var(--ai-accent);
  text-decoration: underline;
  text-underline-offset: 2px;
}
.markdown-content :deep(code) {
  padding: 0.1em 0.35em;
  border-radius: 4px;
  background: var(--bg-overlay-l1);
  font-size: 0.9em;
}
.markdown-content :deep(pre) {
  margin: 0.4em 0;
  padding: 0.6em 0.75em;
  border-radius: var(--radius-8);
  background: var(--bg-overlay-l1);
  overflow-x: auto;
}
.markdown-content :deep(pre code) {
  padding: 0;
  background: transparent;
}
.markdown-content :deep(blockquote) {
  margin: 0.4em 0;
  padding-left: 0.75em;
  border-left: 2px solid var(--ai-bubble-border);
  color: var(--text-secondary);
}
</style>
