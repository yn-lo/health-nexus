<script setup lang="ts">
/**
 * ChatInputBar - 患者端统一底部输入栏
 *
 * 复用于 ChatHome 和 ChatConversation，保持输入区样式一致。
 * - 磨玻璃 ai-input-card 容器
 * - 科室选择 pill
 * - textarea 自动增高（上限 40vh）
 * - 发送/停止按钮（流式中变为停止按钮，emit('stop')）
 * 通过 emit('send', text) 向父组件传递输入内容
 */
import { computed, nextTick, ref } from 'vue'
import { Send, Square, Stethoscope } from '@lucide/vue'

const props = withDefaults(defineProps<{
 /** 科室名称 */
 departmentName?: string
 /** 发送按钮 loading（流式中） */
 loading?: boolean
 /** 占位文字 */
 placeholder?: string
}>(), {
 departmentName: '全部科室',
 loading: false,
 placeholder: '请输入您的健康问题...',
})

const emit = defineEmits<{
 send: [text: string]
 stop: []
 openDeptPicker: []
}>()

const inputText = ref('')
const textareaRef = ref<HTMLTextAreaElement | null>(null)

const canSend = computed(() => inputText.value.trim().length > 0 && !props.loading)

function autoResize() {
 const el = textareaRef.value
 if (!el) return
 el.style.height = 'auto'
 el.style.height = `${Math.min(el.scrollHeight, window.innerHeight * 0.4)}px`
}

function onInputKeydown(e: KeyboardEvent) {
 if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
 e.preventDefault()
 doSend()
 }
}

function doSend() {
 const text = inputText.value.trim()
 if (!text || props.loading) return
 emit('send', text)
 inputText.value = ''
 nextTick(() => autoResize())
}

/** 父组件可调用以设置初始问题 */
function setText(text: string) {
 inputText.value = text
 nextTick(() => autoResize())
}

defineExpose({ setText, inputText })
</script>

<template>
 <div class="chat-input-bar px-[var(--spacer-20)] pb-[calc(var(--spacer-16)+env(safe-area-inset-bottom,0px))]">
 <div class="ai-input-card bg-[var(--bg-base-default)] rounded-[var(--radius-20)] p-[var(--spacer-12)]">
 <!-- 科室选择 pill -->
 <button
 class="inline-flex items-center gap-[var(--spacer-4)] h-[26px] px-[var(--spacer-10)] mb-[var(--spacer-10)] rounded-[var(--radius-full)] bg-[var(--bg-brand-light)] text-text-brand text-body-xs-strong font-medium whitespace-nowrap transition-colors"
 :style="{ transitionDuration: 'var(--micro-duration)' }"
 aria-label="选择科室"
 @click="emit('openDeptPicker')"
 >
 <Stethoscope :size="12" class="text-icon-brand" />
 <span class="text-text-brand">{{ departmentName }}</span>
 </button>

 <!-- 输入栏 - 内嵌发送按钮 -->
 <div class="flex items-end gap-[var(--spacer-8)]">
 <textarea
 ref="textareaRef"
 v-model="inputText"
 rows="1"
 class="ai-textarea flex-1 min-w-0 resize-none bg-transparent border-none outline-none text-body-base text-text placeholder:text-text-tertiary"
 :placeholder="placeholder"
 aria-label="输入健康问题"
 :maxlength="2000"
 @input="autoResize"
 @keydown="onInputKeydown"
 />
 <button
 class="ai-send-btn shrink-0 flex items-center justify-center rounded-[var(--radius-full)] bg-brand-light text-icon-brand disabled:bg-brand-disabled disabled:text-tertiary"
 :aria-label="loading ? '停止生成' : '发送'"
 :disabled="!canSend && !loading"
 @click="loading ? emit('stop') : doSend()"
 >
 <Square v-if="loading" :size="12" fill="currentColor" />
 <Send v-else :size="20" class="text-icon-brand" />
 </button>
 </div>
 </div>
 </div>
</template>

<style scoped ponytail:allow-scoped-css 组件级样式覆盖，折中>
/* ── 自动增高 textarea ───────────────────────────────────── */
.ai-textarea {
 max-height: 40vh;
 padding: 0;
 overflow-y: auto;
}

/* ── 发送按钮 hover/press ─────────────── */
.ai-send-btn {
 width: 30px;
 height: 30px;
 box-shadow: var(--shadow-glow-btn);
 transition: transform var(--micro-duration) var(--micro-ease),
 background-color var(--micro-duration) var(--micro-ease),
 box-shadow var(--micro-duration) var(--micro-ease);
}
.ai-send-btn:hover:not(:disabled) {
 transform: scale(var(--hover-scale-strong));
}
.ai-send-btn:active:not(:disabled) {
 transform: scale(var(--press-scale));
}
.ai-send-btn:disabled {
 box-shadow: none;
 cursor: not-allowed;
}
.ai-send-btn svg {
 padding: 1px;
}

@media (prefers-reduced-motion: reduce) {
 .ai-send-btn {
 transition: none;
 }
 .ai-send-btn:hover:not(:disabled) {
 transform: none;
 }
}
</style>
