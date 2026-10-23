<script setup lang="ts">
import { ref } from 'vue'
import { Copy, Sparkles } from '@lucide/vue'
import MessageContent from './MessageContent.vue'
import ReferenceList from './ReferenceList.vue'
import { useCiteScroll } from '@/chat/composables/useCiteScroll'
import type { Reference } from '@/shared/types/chat'

withDefaults(defineProps<{
  content: string
  references?: Reference[]
  thinking?: boolean
  label: string
}>(), {
  references: () => [],
  thinking: false,
})

const emit = defineEmits<{
  copy: [content: string]
  openArticle: [articleId: number]
}>()

const root = ref<HTMLElement | null>(null)
const { scrollToCite } = useCiteScroll(root)
</script>

<template>
  <div ref="root" class="chat-row chat-row--ai">
    <div class="ds-avatar ds-avatar--brand mt-[var(--spacer-4)]">
      <Sparkles :size="14" />
    </div>
    <div class="flex-1 min-w-0">
      <div class="chat-assistant-name">
        <span class="font-medium text-text">健康助手</span>
        <span class="chat-time">{{ label }}</span>
      </div>

      <!-- 思考中：3-dot pulse -->
      <div v-if="thinking" class="ds-bubble ds-bubble--ai">
        <div class="flex items-center gap-[var(--spacer-6)]">
          <span class="thinking-dot" />
          <span class="thinking-dot" />
          <span class="thinking-dot" />
        </div>
      </div>

      <!-- 流式输出 -->
      <template v-else-if="content">
        <div class="ds-bubble ds-bubble--ai">
          <MessageContent
            :content="content"
            :references="references"
            streaming
            @cite="scrollToCite"
          />
        </div>
        <ReferenceList
          :content="content"
          :references="references"
          @open="emit('openArticle', $event)"
        />
        <div class="feedback-bar flex items-center mt-[var(--spacer-10)]">
          <button
            class="feedback-btn ml-auto flex items-center justify-center text-icon-tertiary"
            aria-label="复制内容"
            @click="emit('copy', content)"
          >
            <Copy :size="15" />
          </button>
        </div>
      </template>
    </div>
  </div>
</template>
