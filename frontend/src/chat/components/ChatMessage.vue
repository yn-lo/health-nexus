<script setup lang="ts">
import { ref } from 'vue'
import { Copy, Sparkles } from '@lucide/vue'
import MessageContent from './MessageContent.vue'
import ReferenceList from './ReferenceList.vue'
import { fmtChatTime } from '@/shared/utils/format'
import { useCiteScroll } from '@/chat/composables/useCiteScroll'
import { FEEDBACK_OPTIONS, type MessageFeedback } from '@/shared/constants/feedback'
import type { Message } from '@/shared/types/chat'

defineProps<{
  message: Message
  feedback?: string
  canFeedback: boolean
  grouped: boolean
}>()

const emit = defineEmits<{
  feedback: [value: MessageFeedback]
  copy: [content: string]
  openArticle: [articleId: number]
}>()

const root = ref<HTMLElement | null>(null)
const { scrollToCite } = useCiteScroll(root)
</script>

<template>
  <div
    v-if="message.role === 'user'"
    ref="root"
    class="chat-row chat-row--user"
    :class="{ 'chat-row--grouped': grouped }"
  >
    <div class="chat-row__content chat-row__content--user">
      <div class="ds-bubble ds-bubble--user">
        <MessageContent :content="message.content" />
      </div>
      <span class="chat-time">{{ fmtChatTime(message.created_at) }}</span>
    </div>
  </div>

  <div
    v-else-if="message.role === 'assistant'"
    ref="root"
    class="chat-row chat-row--ai"
    :class="{ 'chat-row--grouped': grouped }"
  >
    <div class="ds-avatar ds-avatar--brand mt-[var(--spacer-4)]">
      <Sparkles :size="14" />
    </div>
    <div class="flex-1 min-w-0 chat-row__content">
      <div class="chat-assistant-name">
        <span class="font-medium text-text">健康助手</span>
        <span class="chat-time">{{ fmtChatTime(message.created_at) }}</span>
        <!-- 结果码来自服务端权威结果：截断的回答标注不完整，刷新后仍然可见 -->
        <span v-if="message.result_code === 'PARTIAL'" class="chat-incomplete">回答不完整</span>
      </div>
      <div class="ds-bubble ds-bubble--ai">
        <MessageContent
          :content="message.content"
          :references="message.references"
          @cite="scrollToCite"
        />
      </div>

      <ReferenceList
        :content="message.content"
        :references="message.references"
        @open="emit('openArticle', $event)"
      />

      <!-- 反馈栏（匿名/未同步消息无服务端持久化，仅保留复制）：三态靠左、"/" 分隔；复制固定最右 -->
      <div class="feedback-bar flex items-center mt-[var(--spacer-10)]">
        <div v-if="canFeedback" class="flex items-center gap-[var(--spacer-4)]">
          <template v-for="(opt, idx) in FEEDBACK_OPTIONS" :key="opt.value">
            <span v-if="idx > 0" class="text-body-xs text-icon-tertiary select-none" aria-hidden="true">/</span>
            <button
              type="button"
              class="feedback-pill text-body-xs"
              :class="feedback === opt.value
                ? 'bg-[var(--bg-brand-light)] text-text-brand'
                : 'text-icon-tertiary'"
              :aria-pressed="feedback === opt.value"
              :aria-label="opt.label"
              @click="emit('feedback', opt.value)"
            >
              {{ opt.label }}
            </button>
          </template>
        </div>
        <button
          class="feedback-btn ml-auto flex items-center justify-center text-icon-tertiary"
          aria-label="复制内容"
          @click="emit('copy', message.content)"
        >
          <Copy :size="15" />
        </button>
      </div>
    </div>
  </div>
</template>
