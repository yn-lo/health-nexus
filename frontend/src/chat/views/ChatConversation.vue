<script setup lang="ts">
/**
 * ChatConversation 对话页 - AI-Native UI 风格
 *
 * 风格升级（参考 ui-ux-pro-max styles.csv #43 AI-Native UI）：
 *   - 顶部使用统一 ChatHeader（frosted variant + 返回 + 标题 + 历史）
 *   - 用户气泡：#E0E7FF indigo-100 + #1E1B4B 文本（右对齐）
 *   - AI 气泡：#F9FAFB 灰 + indigo accent 头像（左对齐）
 *   - 消息间距 16px（var(--message-gap)）
 *   - 底部输入栏使用统一 ChatInputBar（fixed bottom + 胶囊圆角）
 *   - 3 点输入指示器（pulse 动画）
 *   - 流式光标（光标闪烁）
 *   - 移除 van-nav-bar，避免 Vant 默认蓝色干扰
 * 保留功能：useSSEChat、useDepartments、ChatHistoryDrawer、点踩原因
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ActionSheet as VanActionSheet, showDialog, showFailToast, showToast } from 'vant'
import {
  Copy,
  Sparkles,
  ThumbsDown,
  ThumbsUp,
  User,
} from '@lucide/vue'
import { useDepartments } from '@/chat/composables/useDepartments'
import { useSSEChat } from '@/chat/composables/useSSEChat'
import { useChatStore } from '@/stores/chat'
import { DisclaimerFooter } from '@/shared/components'
import ChatHeader from '@/chat/components/ChatHeader.vue'
import ChatInputBar from '@/chat/components/ChatInputBar.vue'
import ChatHistoryDrawer from '@/chat/components/ChatHistoryDrawer.vue'
import DeptPickerPopup from '@/chat/components/DeptPickerPopup.vue'
import KnowledgeList from '@/chat/views/KnowledgeList.vue'
import MarkdownIt from 'markdown-it'
import { submitMessageFeedback } from '@/shared/api/chat'
import { errmsg } from '@/shared/api/client'
import type { Message, Reference } from '@/shared/types/chat'

const router = useRouter()
const route = useRoute()
const chatStore = useChatStore()

const md = new MarkdownIt({ html: false, breaks: true, linkify: true })

/** 渲染 markdown 为 HTML（禁用 raw HTML 防注入） */
function renderMd(text: string): string {
  return md.render(text)
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

function onHistoryNewChat() {
  router.push({ name: 'chat-home' })
  showHistory.value = false
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
  const convId = sseConversationId.value || conversationId.value
  if (convId) {
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

    <!-- 消息列表 - AI-Native：16px 间距 -->
    <main v-if="activeMode === 'chat'" ref="messageListRef" class="flex-1 overflow-y-auto px-[var(--spacer-12)] py-[var(--spacer-16)] no-scrollbar" :style="{ paddingBottom: `${inputBarHeight}px` }">
      <div class="flex flex-col gap-[var(--message-gap)]">
        <template v-for="msg in chatStore.messages" :key="msg.id">
          <!-- 用户消息 - AI-Native: indigo-100 气泡 + indigo-950 文本 -->
          <div v-if="msg.role === 'user'" class="ai-message flex justify-end gap-[var(--spacer-8)] items-start">
            <div class="ds-bubble ds-bubble--user">
              <div class="m-0 break-words text-[14px] leading-[20px] markdown-content" v-html="renderMd(msg.content)"></div>
            </div>
            <div class="ds-avatar ds-avatar--xs mt-[var(--spacer-4)]">
              <User :size="14" />
            </div>
          </div>

          <!-- AI 消息 - AI-Native: 灰气泡 + indigo 渐变头像 -->
          <div v-else-if="msg.role === 'assistant'" class="ai-message flex items-start gap-[var(--spacer-8)]">
            <div class="ds-avatar ds-avatar--xs ds-avatar--brand mt-[var(--spacer-4)]">
              <Sparkles :size="14" />
            </div>
            <div class="flex-1 min-w-0">
              <div class="ds-bubble ds-bubble--ai ds-bubble--ai-lg">
                <div class="m-0 break-words text-[14px] leading-[20px] markdown-content" v-html="renderMd(msg.content)"></div>
              </div>

              <!-- 引用来源 -->
              <div v-if="msg.references?.length" class="mt-[var(--spacer-8)]">
                <p class="text-body-xs text-text-tertiary m-0 mb-[var(--spacer-4)]">参考来源</p>
                <div class="flex flex-col gap-[var(--spacer-4)]">
                  <div
                    v-for="ref in dedupeRefs(msg.references)"
                    :key="ref.article_id"
                    class="ref-card flex items-center gap-[var(--spacer-6)] rounded-[var(--radius-8)] bg-[var(--bg-overlay-l1)] px-[var(--spacer-10)] py-[var(--spacer-6)] transition-transform active:scale-[0.98]"
                    @click="goToArticle(ref.article_id)"
                  >
                    <span class="ref-card__dot" />
                    <p class="flex-1 min-w-0 text-body-xs text-text-secondary m-0 leading-snug line-clamp-2">
                      {{ ref.article_title }}
                    </p>
                  </div>
                </div>
              </div>

              <!-- 反馈栏 -->
              <div class="feedback-bar flex items-center mt-[var(--spacer-12)]">
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

        <!-- 思考中指示器 - AI-Native 3-dot pulse -->
        <div v-if="isThinking" class="flex items-start gap-[var(--spacer-8)]">
          <div class="ds-avatar ds-avatar--xs ds-avatar--brand mt-[var(--spacer-4)]">
            <Sparkles :size="14" />
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

        <!-- 流式输出中的 AI 消息 -->
        <div v-if="isStreaming && currentContent" class="flex items-start gap-[var(--spacer-8)]">
          <div class="ds-avatar ds-avatar--xs ds-avatar--brand mt-[var(--spacer-4)]">
            <Sparkles :size="14" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="ds-bubble ds-bubble--ai ds-bubble--ai-lg">
              <div class="m-0 break-words text-[14px] leading-[20px] markdown-content" v-html="renderMd(currentContent) + '<span class=\'streaming-cursor\'></span>'"></div>
            </div>
            <!-- 流式中的引用来源 -->
            <div v-if="references.length" class="mt-[var(--spacer-8)]">
              <p class="text-body-xs text-text-tertiary m-0 mb-[var(--spacer-4)]">参考来源</p>
              <div class="flex flex-col gap-[var(--spacer-4)]">
                <div
                  v-for="ref in dedupeRefs(references)"
                  :key="ref.article_id"
                  class="ref-card flex items-center gap-[var(--spacer-6)] rounded-[var(--radius-8)] bg-[var(--bg-overlay-l1)] px-[var(--spacer-10)] py-[var(--spacer-6)] transition-transform active:scale-[0.98]"
                  @click="goToArticle(ref.article_id)"
                >
                  <span class="ref-card__dot" />
                  <p class="flex-1 min-w-0 text-body-xs text-text-secondary m-0 leading-snug line-clamp-2">
                    {{ ref.article_title }}
                  </p>
                </div>
              </div>
            </div>
            <!-- 流式中仅保留复制按钮 -->
            <div class="feedback-bar flex items-center mt-[var(--spacer-12)]">
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
      @new-chat="onHistoryNewChat"
    />

    <!-- 科室选择弹窗 -->
    <DeptPickerPopup
      v-model:show="showDeptPicker"
      :departments="departments"
      :selected-id="selectedDepartmentId"
      @select="onDeptSelect"
    />

    <!-- 点踩原因选择 -->
    <VanActionSheet
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

/* ── Micro-interactions：消息 fade-in ──────────────────── */
.ai-message {
  animation: message-fade-in 200ms var(--micro-ease) both;
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

/* ── 引用来源卡片：品牌色圆点 + 触感按压 ───────────────── */
.ref-card__dot {
  width: 6px;
  height: 6px;
  border-radius: var(--radius-full);
  background: var(--ai-accent);
  flex-shrink: 0;
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
  .ai-message {
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
