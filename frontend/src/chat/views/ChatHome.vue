<script setup lang="ts">
/**
 * ChatHome 聊天首页 - AI Healthcare Native 综合风格
 *
 * 融合 4 种风格特长：
 * - AI-Native UI -> 浅灰底 #F5F5F5 / clean input / context cards border-left
 * - Hero-Centric -> 96px hero orb + 双层 pulse 光晕 + 渐变头像
 * - Micro-interactions-> 100ms hover scale 1.02 / press scale 0.97 / 输入卡片 focus 增强
 * - Accessible & Ethical -> 3px focus ring / 44px 触摸目标 / reduced-motion
 * 保留功能：科室选择、历史抽屉、推荐问题、SSE 触发
 */
import { computed, ref, watch, type Component } from 'vue'
import { storeToRefs } from 'pinia'
import { useRouter } from 'vue-router'
import {
 Activity,
 ChevronRight,
 Droplet,
 HeartPulse,
 Pill,
 Sparkles,
} from '@lucide/vue'
import { useDsToast } from '@/shared/composables/useDsToast'
import { useDepartments } from '@/chat/composables/useDepartments'
import { useChatMode } from '@/chat/composables/useChatMode'
import { DisclaimerFooter } from '@/shared/components'
import ChatHeader from '@/chat/components/ChatHeader.vue'
import ChatModeSwitcher from '@/chat/components/ChatModeSwitcher.vue'
import ChatInputBar from '@/chat/components/ChatInputBar.vue'
import ChatHistoryDrawer from '@/chat/components/ChatHistoryDrawer.vue'
import DeptPickerPopup from '@/chat/components/DeptPickerPopup.vue'
import KnowledgeList from '@/chat/views/KnowledgeList.vue'
import { useBrandStore } from '@/stores/brand'

const router = useRouter()
const { brand } = storeToRefs(useBrandStore())
/** 助手称呼：配置了单位名称则落款到机构（{单位名称}智能助手），否则回退「智能健康助手」 */
const assistantName = computed(() =>
  brand.value.org_name ? `${brand.value.org_name}智能助手` : '智能健康助手',
)
const { departments, selectedDepartmentId, activeDepartment, selectDepartment } = useDepartments({ initialDepartmentId: 0, includeAll: false })

const { showToast } = useDsToast()
/** 提示当前生效的知识库；科室未加载完成时静默，等 name watcher 首次就绪再提示 */
function notifyCurrentDept() {
  const name = activeDepartment.value?.name
  if (name) showToast(`当前知识库：${name}`)
}

// 知识库提示改为一过式 toast：进入页面（科室加载完成）自动提示一次，切换科室后再提示一次。
// 监听 name 而非整个对象，避免科室数组被重新赋值时同样的名字重复弹窗。
watch(
 () => activeDepartment.value?.name,
 (name) => {
  if (name) notifyCurrentDept()
 },
)

const showHistory = ref(false)
const showDeptPicker = ref(false)

/** 视图模式：聊天 / 知识库 — 同步到 URL query，跨导航（如文章详情返回）可恢复 */
const { activeMode, setMode } = useChatMode()

// 从知识库切回聊天模式时，即使科室没有变化也提示一次当前知识库
watch(activeMode, (mode, prev) => {
  if (mode === 'chat' && prev === 'knowledge') notifyCurrentDept()
})

interface QuickAction {
 label: string
 value: string
 icon: Component
}

const recommendedQuestions: QuickAction[] = [
  { label: '高血压怎么吃更安全', value: '高血压怎么吃更安全', icon: HeartPulse },
  { label: '糖尿病日常怎么管理', value: '糖尿病日常怎么管理', icon: Droplet },
  { label: '术后康复注意什么', value: '术后康复注意什么', icon: Activity },
  { label: '感冒发烧怎么用药', value: '感冒发烧怎么用药', icon: Pill },
]

function openHistory() {
 showHistory.value = true
}

function onHistorySelect(conversationId: string) {
 router.push({ name: 'chat-conversation', params: { id: conversationId } })
}

function onHistoryNewChat() {
  // 已在首页即处于全新对话；仅关闭抽屉，避免「新对话」按钮后抽屉残留打开
  showHistory.value = false
}

function openDeptPicker() {
 showDeptPicker.value = true
 // 打开科室选择器即提示当前知识库，便于确认当前生效范围（不依赖是否有值变化）
 notifyCurrentDept()
}

function onDeptSelect(id: number) {
 const unchanged = id === selectedDepartmentId.value
 selectDepartment(id)
 // 选择同一科室不会触发 name watcher，这里补一次提示
 if (unchanged) notifyCurrentDept()
}

function sendMessage(text: string) {
 router.push({
 name: 'chat-conversation',
 params: {},
 query: { q: text, department: String(selectedDepartmentId.value) },
 })
}

function onPromptClick(item: QuickAction) {
 sendMessage(item.value)
}
</script>

<template>
 <div class="chat-home">
 <!-- 顶部栏 -->
 <ChatHeader variant="frosted" @open-history="openHistory">
 <template #center>
 <ChatModeSwitcher :active-mode="activeMode" @update:mode="setMode" />
 </template>
 </ChatHeader>

 <!-- 聊天模式 -->
 <div v-if="activeMode === 'chat'" class="chat-home-body px-[var(--spacer-20)]">
 <!-- Hero — 现代化欢迎区：留白 + 轻法式问候 + 呼吸感 orb -->
 <section class="flex flex-col items-center text-center pt-[var(--spacer-32)] pb-[var(--spacer-32)]">
 <div class="ai-orb-wrapper" aria-hidden="true">
 <div class="ai-orb-pulse-outer" />
 <div class="ai-orb-pulse-inner" />
 <div class="ai-orb flex items-center justify-center">
 <Sparkles class="w-9 h-9 text-onbrand" />
 </div>
 </div>

 <h2 class="hero-headline text-center font-semibold leading-[1.3] text-text break-words text-[var(--hero-headline-size)] mt-[var(--spacer-24)]">
 您好，我是您的<span class="hero-headline-accent">{{ assistantName }}</span>
 </h2>
 <p class="text-center text-body-base text-text-secondary max-w-[300px] leading-[1.6] mt-[var(--spacer-12)]">
 7×24 小时问答，答案可溯源
 </p>
 </section>

 <!-- 向我提问 - 现代化推荐卡片 -->
 <section class="pb-[var(--spacer-20)]">
 <div class="flex flex-col gap-[var(--spacer-10)]">
 <button
 v-for="q in recommendedQuestions"
 :key="q.label"
 class="ai-context-card group flex items-center w-full text-left bg-[var(--bg-base-default)] rounded-[var(--radius-16)] px-[var(--spacer-16)] py-[var(--spacer-14,14px)] gap-[var(--spacer-12)] shadow-[var(--shadow-xs)] ring-1 ring-[var(--border-neutral-l1)]"
 @click="onPromptClick(q)"
 >
 <span class="ai-context-icon shrink-0 flex items-center justify-center w-11 h-11 rounded-[var(--radius-12)] bg-[var(--bg-brand-light)] text-icon-brand">
 <component :is="q.icon" :size="22" />
 </span>
 <span class="flex-1 min-w-0 text-body-base text-text font-medium leading-snug">{{ q.label }}</span>
 <ChevronRight :size="18" class="shrink-0 text-icon-tertiary transition-transform duration-200 group-hover:translate-x-0.5" />
 </button>
 </div>
 </section>

 <!-- 免责声明 -->
 <DisclaimerFooter />
 </div>

 <!-- 输入卡片 - 悬浮底部 -->
 <ChatInputBar
 v-if="activeMode === 'chat'"
 class="chat-home-input-bar"
 :department-name="activeDepartment?.name"
 @send="sendMessage"
 @open-dept-picker="openDeptPicker"
 />

 <!-- 知识库模式 -->
 <KnowledgeList v-else embedded />

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
 </div>
</template>

<style scoped ponytail:allow-scoped-css 组件级样式覆盖，折中>
.chat-home {
 background: var(--bg-base-default);
 min-height: 100dvh;
 display: flex;
 flex-direction: column;
}

/* ── 聊天模式：内容区可滚动，输入栏悬浮底部 ──────────────── */
.chat-home-body {
 flex: 1;
 overflow-y: auto;
 padding-bottom: 120px;
}

.chat-home-input-bar {
 position: fixed;
 bottom: 0;
 left: 0;
 right: 0;
 z-index: 20;
 background: var(--bg-base-default);
}

/* ── Hero-Centric：96px orb + 双层 pulse 光晕 ─────────────── */
.ai-orb-wrapper {
 position: relative;
 width: var(--hero-orb-size);
 height: var(--hero-orb-size);
 display: flex;
 align-items: center;
 justify-content: center;
}

.ai-orb {
 position: relative;
 z-index: 2;
 width: 70px;
 height: 70px;
 border-radius: var(--radius-full);
 background: var(--ai-avatar-gradient);
 box-shadow:
 0 8px 24px var(--hero-glow-color),
 0 2px 8px var(--hero-glow-color-soft);
 display: flex;
 align-items: center;
 justify-content: center;
}

.ai-orb-pulse-outer {
 position: absolute;
 inset: 0;
 border-radius: var(--radius-full);
 background: var(--hero-glow-color-soft);
 animation: ai-pulse-outer var(--hero-orb-pulse-duration) ease-out infinite;
}

.ai-orb-pulse-inner {
 position: absolute;
 inset: 8px;
 border-radius: var(--radius-full);
 background: var(--hero-glow-color);
 animation: ai-pulse-inner var(--hero-orb-pulse-inner-duration) ease-out infinite;
}

@keyframes ai-pulse-outer {
 0% { transform: scale(0.9); opacity: 0.7; }
 100% { transform: scale(1.4); opacity: 0; }
}

@keyframes ai-pulse-inner {
 0% { transform: scale(0.95); opacity: 0.6; }
 100% { transform: scale(1.25); opacity: 0; }
}

/* ── Hero 标题：品牌色点睛词（克制，仅一处强调） ──────────── */
.hero-headline-accent {
 color: var(--text-brand);
 font-weight: 600;
}

/* ── 推荐卡片：更显著的按压反馈 ───────────────────────────── */
.ai-context-card {
 box-shadow: var(--shadow-xs);
}
</style>
