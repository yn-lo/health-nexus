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
import { ref, type Component } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
 Activity,
 ChevronRight,
 Droplet,
 HeartPulse,
 Pill,
 Sparkles,
} from '@lucide/vue'
import { useDepartments } from '@/chat/composables/useDepartments'
import { DisclaimerFooter } from '@/shared/components'
import ChatHeader from '@/chat/components/ChatHeader.vue'
import ChatInputBar from '@/chat/components/ChatInputBar.vue'
import ChatHistoryDrawer from '@/chat/components/ChatHistoryDrawer.vue'
import DeptPickerPopup from '@/chat/components/DeptPickerPopup.vue'
import KnowledgeList from '@/chat/views/KnowledgeList.vue'

const router = useRouter()
const route = useRoute()
const { departments, selectedDepartmentId, activeDepartment, selectDepartment } = useDepartments({ initialDepartmentId: 0 })

const showHistory = ref(false)
const showDeptPicker = ref(false)

/** 视图模式：聊天 / 知识库 — 同步到 URL query，跨导航（如文章详情返回）可恢复 */
const activeMode = ref<'chat' | 'knowledge'>(route.query.mode === 'knowledge' ? 'knowledge' : 'chat')
const knowledgeRef = ref<InstanceType<typeof KnowledgeList> | null>(null)

/** 切换模式时同步到 URL（replace 避免堆历史） */
function setMode(mode: 'chat' | 'knowledge') {
 if (activeMode.value === mode) return
 activeMode.value = mode
 router.replace({ query: { ...route.query, mode } })
}

interface QuickAction {
 label: string
 value: string
 icon: Component
}

const recommendedQuestions: QuickAction[] = [
 { label: '高血压患者饮食注意事项', value: '高血压患者饮食注意事项', icon: HeartPulse },
 { label: '糖尿病日常管理建议', value: '糖尿病日常管理建议', icon: Droplet },
 { label: '术后康复需要注意什么', value: '术后康复需要注意什么', icon: Activity },
 { label: '感冒发烧如何正确用药', value: '感冒发烧如何正确用药', icon: Pill },
]

function openHistory() {
 showHistory.value = true
}

function onHistorySelect(conversationId: string) {
 router.push({ name: 'chat-conversation', params: { id: conversationId } })
}

function onHistoryNewChat() {
 // 已在首页，无需操作
}

function openDeptPicker() {
 showDeptPicker.value = true
}

function onDeptSelect(id: number) {
 selectDepartment(id)
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

 <h2 class="hero-headline text-center font-semibold leading-[1.3] text-text break-keep text-[var(--hero-headline-size)] mt-[var(--spacer-24)]">
 您好，我是您的<span class="hero-headline-accent">智能健康助手</span>
 </h2>
 <p class="text-center text-body-base text-text-secondary max-w-[300px] leading-[1.6] mt-[var(--spacer-12)]">
 随时解答健康疑问，提供可信医学参考
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
 :department-name="activeDepartment.name"
 @send="sendMessage"
 @open-dept-picker="openDeptPicker"
 />

 <!-- 知识库模式 -->
 <KnowledgeList v-else ref="knowledgeRef" embedded />

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

/* ── A11y：减弱动效偏好 ─────────────────────────────────── */
@media (prefers-reduced-motion: reduce) {
 .ai-orb-pulse-outer,
 .ai-orb-pulse-inner {
 animation: none;
 }
 .ai-context-card:hover {
 transform: none;
 }
}
</style>
