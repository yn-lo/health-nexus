<script setup lang="ts">
/**
 * ChatConversation 对话页 - AI-Native UI（消息渲染/反馈/流式展示已拆为组件）
 * 本文件只保留：路由与会话生命周期、SSE 编排、滚动、匿名本地快照。
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useDepartments } from '@/chat/composables/useDepartments'
import { useSSEChat } from '@/chat/composables/useSSEChat'
import { useChatMode } from '@/chat/composables/useChatMode'
import { useMessageFeedback } from '@/chat/composables/useMessageFeedback'
import { useChatStore, loadAnonMessages, saveAnonSessionSnapshot } from '@/stores/chat'
import { isAuthenticated } from '@/shared/api/client'
import { DisclaimerFooter } from '@/shared/components'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { type MessageFeedback } from '@/shared/constants/feedback'
import type { Message } from '@/shared/types/chat'
import ChatHeader from '@/chat/components/ChatHeader.vue'
import ChatModeSwitcher from '@/chat/components/ChatModeSwitcher.vue'
import ChatInputBar from '@/chat/components/ChatInputBar.vue'
import ChatHistoryDrawer from '@/chat/components/ChatHistoryDrawer.vue'
import ChatMessage from '@/chat/components/ChatMessage.vue'
import ChatStreamingMessage from '@/chat/components/ChatStreamingMessage.vue'
import DeptPickerPopup from '@/chat/components/DeptPickerPopup.vue'
import KnowledgeBrowser from '@/chat/components/KnowledgeBrowser.vue'

const router = useRouter()
const route = useRoute()
const chatStore = useChatStore()
const { showToast, showFailToast } = useDsToast()
const { showDialog } = useDsDialog()

const inputBarRef = ref<InstanceType<typeof ChatInputBar> | null>(null)
const messageListRef = ref<HTMLDivElement>()
const inputBarHeight = ref(120)
let inputBarObserver: ResizeObserver | null = null
const showHistory = ref(false)
const showDeptPicker = ref(false)
/** 本轮用户消息的本地乐观 ID：result 事件到达后替换为服务端权威 ID（反馈等操作据此定位真实消息） */
let pendingUserLocalId = ''

const { activeMode, setMode } = useChatMode()

const initialDepartmentId = Number(route.query.department ?? 0)
// 问答必须聚焦具体责任科室：不提供"全部科室"。未指定时由科室列表的 is_default（公共科室）预选。
const { departments, selectedDepartmentId, activeDepartment, selectDepartment } = useDepartments({ initialDepartmentId, includeAll: false })

// SSE: sseOptions 为可变对象，route 变化时更新 conversationId
const sseOptions = { conversationId: (route.params.id as string) ?? '', selectedDeptId: selectedDepartmentId.value }
// 科室列表异步加载后会把默认选中改为具体科室（is_default）；同步给 SSE 选项，
// 保证新建会话携带的 selected_dept_id 与 UI 展示一致。
watch(selectedDepartmentId, (id) => { sseOptions.selectedDeptId = id })
const {
  isStreaming, currentContent, references, notices, crisis, error, result, stage, aborted,
  conversationId: sseConversationId, sendQuestion, abort, dismissNotice,
} = useSSEChat(sseOptions)

const conversationId = computed(() => (route.params.id as string) ?? '')
const isThinking = computed(() => isStreaming.value && !currentContent.value)

/** 后端 stage 事件 → 思考态状态文案；未知/尚未到达任一阶段时回退为"正在思考"。 */
const STAGE_LABELS: Record<string, string> = {
  understanding: '正在理解问题',
  retrieving: '正在检索知识',
  composing: '正在组织回答',
  reviewing: '正在核对内容',
}
const thinkingLabel = computed(() => STAGE_LABELS[stage.value] ?? '正在思考')

// 匿名用户（无 access token）：走 /api/public/chat/stream，上下文存服务端 Redis 12h，
// 前端无公开消息拉取/反馈端点——消息本地回显（localStorage），隐藏反馈入口，避免 401 与无效反馈。
const isAnon = computed(() => !isAuthenticated.value)
const { feedbackMap, canFeedback, syncFromMessages, submit: submitFeedback } = useMessageFeedback(isAnon)

/** 匿名会话：将当前消息列表持久化到 localStorage（key 绑定会话 id），刷新后回显 */
function persistAnonMessages() {
  if (!isAnon.value) return
  const convId = sseConversationId.value || conversationId.value
  if (convId) saveAnonSessionSnapshot(convId, chatStore.messages)
}

function openHistory() {
  showHistory.value = true
}

/** 向上加载更早消息；保持视口锚定（加载后按新增高度回补 scrollTop，避免跳到顶部） */
async function loadEarlier() {
  const convId = sseConversationId.value || conversationId.value
  if (!convId || chatStore.loading) return
  const el = messageListRef.value
  const prevHeight = el?.scrollHeight ?? 0
  await chatStore.loadEarlierMessages(convId)
  nextTick(() => {
    if (el) el.scrollTop += el.scrollHeight - prevHeight
  })
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
  // 作废在途的会话加载：否则加载态会残留（输入栏被永久禁用），且其响应回来会把旧会话历史写回
  chatStore.cancelConversationLoad()
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

/** 与下一条消息同角色 → 收拢间距（分组呼吸感） */
function isGroupedWithNext(idx: number): boolean {
  const next = chatStore.messages[idx + 1]
  const cur = chatStore.messages[idx]
  if (!cur || !next) return false
  return cur.role === next.role
}

function sendMessage(text: string) {
  // 会话历史未就位时禁止发送：否则消息会被追加到上一条会话的残列表，且新会话历史返回后被整体丢弃（P1）
  if (!text || isStreaming.value || chatStore.messagesLoading) return

  pendingUserLocalId = `local-${crypto.randomUUID()}`
  chatStore.addMessage({
    id: pendingUserLocalId,
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

/** 跳转到文章详情页 */
function goToArticle(articleId: number) {
  router.push({ name: 'wiki-article', params: { id: articleId } })
}

function onFeedback(msg: Message, value: MessageFeedback) {
  void submitFeedback(msg, value)
}

/** 打开已有会话：先恢复会话详情（含锁定科室）再拉取消息。
 * 科室必须恢复成会话锁定值——否则前端仍以默认「全部科室」展示，且后续请求携带错误科室会被
 * 后端以 CHAT_DEPT_LOCKED(409) 拒绝，历史会话无法续聊。
 *
 * P1 快速切换：从"开始加载"就分配代次（beginConversationLoad），详情、科室、消息共享同一代次。
 * 同代次内消息列表已被清空并进入加载态，历史就位前不允许发送。
 * A 的迟到详情/消息因代次过期被丢弃，不会覆盖 B 的 currentConversation / messages / 科室。 */
async function openConversation(id: string, jumpToBottom = false) {
  const epoch = chatStore.beginConversationLoad()
  try {
    const conv = await chatStore.fetchConversation(id, epoch)
    // 代次已过期（用户已切到其他会话）→ 放弃后续科室/消息更新，避免污染当前视图。
    if (!chatStore.isCurrentLoad(epoch)) return
    // 仅当会话已锁定科室时覆盖选中项；未锁定（历史 NULL）保持默认责任科室，
    // 避免把选中项重置为"全部科室"（问答已取消该选项）。
    if (conv.locked_dept_id != null) selectDepartment(conv.locked_dept_id)
    await chatStore.fetchMessages(id, epoch)
    if (!chatStore.isCurrentLoad(epoch)) return
    syncFromMessages(chatStore.messages)
    scrollToBottom(jumpToBottom)
  } catch {
    if (chatStore.isCurrentLoad(epoch)) {
      chatStore.endConversationLoad(epoch)
      showFailToast('加载消息失败')
    }
  }
}

// Route 变化：中止旧流、更新 conversationId、重新加载会话
watch(conversationId, (newId) => {
  // SSE conversation 事件触发的路由同步：sseConversationId 已是最新，跳过避免中断流
  if (newId === sseConversationId.value) return
  if (isStreaming.value) {
    abort()
    currentContent.value = ''
  }
  sseConversationId.value = newId
  sseOptions.conversationId = newId
  if (!newId) return
  if (isAnon.value) {
    // 匿名：无公开拉取端点，从 localStorage 回显本地缓存（服务端 Redis 上下文此时不展示给用户）
    chatStore.messages = loadAnonMessages(newId)
    scrollToBottom()
    return
  }
  void openConversation(newId)
})

// 后端 conversation 事件下发新会话 ID → 更新路由（URL 反映真实会话，后续消息自动携带）
watch(sseConversationId, (id) => {
  if (id && id !== conversationId.value) {
    router.replace({ name: 'chat-conversation', params: { id } })
  }
})

/**
 * 固化本轮回答：优先采用 result 事件的权威结果（真实消息 ID / 最终结果码 / 最终引用），
 * 不再按内容猜测 ID、也不再整页回拉（回拉会覆盖用户在此期间发送的新消息）。
 * result 缺失（异常中断）时退回本地 ID 与推断结果码，此时该消息暂不支持反馈。
 */
function addTurnMessage(content: string, fallbackCode: string) {
  const res = result.value
  chatStore.addMessage({
    id: res?.assistant_message_id || `local-assistant-${crypto.randomUUID()}`,
    conversation_id: conversationId.value,
    role: 'assistant',
    content,
    result_code: res?.result_code ?? fallbackCode,
    references: res?.references?.length ? res.references : references.value,
    created_at: new Date().toISOString(),
  })
  // 本轮用户消息改用服务端权威 ID（反馈等后续操作据此定位真实消息）
  if (res?.user_message_id && pendingUserLocalId) {
    chatStore.remapMessageId(pendingUserLocalId, res.user_message_id)
    pendingUserLocalId = ''
  }
}

// SSE 结束：将累积内容固化为 AI 消息
watch(isStreaming, (streaming, prev) => {
  if (!prev || streaming) return
  if (error.value) {
    showFailToast(error.value)
    // error 事件时仍将已累积的部分内容固化为消息，避免用户已看到的流式片段丢失。
    // 无权威 result 时用 PARTIAL（"回答不完整"），与后端中断语义一致；INTERCEPTED 仅表示安全拦截。
    if (currentContent.value) addTurnMessage(currentContent.value, 'PARTIAL')
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
      id: result.value?.assistant_message_id || `local-assistant-${crypto.randomUUID()}`,
      conversation_id: conversationId.value,
      role: 'assistant',
      content: crisis.value.answer,
      result_code: result.value?.result_code ?? 'CRISIS',
      references: [],
      created_at: new Date().toISOString(),
    })
    scrollToBottom()
    persistAnonMessages()
    return
  }
  if (currentContent.value) {
    // 主动停止/无完成证据：保留片段同样按 PARTIAL 标注"回答不完整"（result 存在时以服务端结果为准）
    addTurnMessage(currentContent.value, aborted.value ? 'PARTIAL' : 'ANSWERED')
  }
  scrollToBottom()
  persistAnonMessages()
})

// 流式输出时自动滚动
watch(currentContent, () => scrollToBottom())

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
  if (isAnon.value) {
    // 匿名：无公开拉取端点，从 localStorage 回显本地缓存
    chatStore.messages = loadAnonMessages(conversationId.value)
    scrollToBottom(true)
    return
  }
  await openConversation(conversationId.value, true)
})

onUnmounted(() => {
  inputBarObserver?.disconnect()
})
</script>

<template>
  <div class="chat-conversation flex flex-col h-[100dvh] bg-[var(--bg-base-secondary)]">
    <!-- 顶部栏 - 磨玻璃半透明（AppHeader frosted 变体） -->
    <ChatHeader variant="frosted" @open-history="openHistory">
      <template #center>
        <ChatModeSwitcher :active-mode="activeMode" @update:mode="setMode" />
      </template>
    </ChatHeader>

    <!-- 消息列表 - AI-Native：分组留白 + 15px 阅读字号 -->
    <main v-if="activeMode === 'chat'" ref="messageListRef" class="flex-1 overflow-y-auto px-[var(--spacer-16)] py-[var(--spacer-20)] no-scrollbar" :style="{ paddingBottom: `${inputBarHeight}px` }">
      <div class="flex flex-col">
        <!-- 向上分页：仅在还有更早记录时展示（匿名无公开拉取端点，不展示） -->
        <div v-if="!isAnon && chatStore.hasMoreMessages" class="load-earlier flex justify-center">
          <button
            type="button"
            class="load-earlier__btn ds-ghost-pill"
            :disabled="chatStore.loading"
            @click="loadEarlier"
          >
            {{ chatStore.loading ? '加载中…' : '加载更早消息' }}
          </button>
        </div>

        <ChatMessage
          v-for="(msg, idx) in chatStore.messages"
          :key="msg.id"
          :message="msg"
          :feedback="feedbackMap[msg.id]"
          :can-feedback="canFeedback(msg)"
          :grouped="isGroupedWithNext(idx)"
          @feedback="onFeedback(msg, $event)"
          @copy="copyMessage"
          @open-article="goToArticle"
        />

        <!-- 思考中 / 流式输出 -->
        <ChatStreamingMessage
          v-if="isThinking"
          thinking
          content=""
          :label="thinkingLabel"
          @copy="copyMessage"
          @open-article="goToArticle"
        />
        <ChatStreamingMessage
          v-else-if="isStreaming && currentContent"
          :content="currentContent"
          :references="references"
          label="正在生成"
          @copy="copyMessage"
          @open-article="goToArticle"
        />

        <!-- 免责声明 -->
        <DisclaimerFooter />
      </div>
    </main>

    <!-- 知识库模式 -->
    <KnowledgeBrowser v-if="activeMode === 'knowledge'" embedded />

    <!-- 独立提示（紧急就医 / 超时）：不进入答案正文，可关闭 -->
    <div v-if="notices.length" class="chat-notice" :style="{ bottom: `${inputBarHeight}px` }" role="status">
      <div
        v-for="(notice, idx) in notices"
        :key="`${notice.kind}-${idx}`"
        class="chat-notice__item"
        :class="`chat-notice__item--${notice.kind}`"
      >
        <span class="chat-notice__text">{{ notice.text }}</span>
        <button type="button" class="chat-notice__close" aria-label="关闭提示" @click="dismissNotice(idx)">×</button>
      </div>
    </div>

    <!-- 底部输入栏 - AI-Native：fixed bottom -->
    <ChatInputBar
      v-if="activeMode === 'chat'"
      ref="inputBarRef"
      class="chat-conversation-input-bar"
      :department-name="activeDepartment?.name"
      :loading="isStreaming"
      :disabled="chatStore.messagesLoading"
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
  </div>
</template>
