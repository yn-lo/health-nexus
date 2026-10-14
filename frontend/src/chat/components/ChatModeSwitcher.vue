<script setup lang="ts">
import { type ChatMode } from '@/chat/composables/useChatMode'

defineProps<{ activeMode: ChatMode }>()
const emit = defineEmits<{ (e: 'update:mode', mode: ChatMode): void }>()
</script>

<template>
  <div class="ds-fab-segment ds-fab-segment--neutral">
    <button
      type="button"
      class="ds-fab-segment__btn"
      :class="{ 'ds-fab-segment__btn--active': activeMode === 'chat' }"
      @click="emit('update:mode', 'chat')"
    >
      聊天
    </button>
    <button
      type="button"
      class="ds-fab-segment__btn"
      :class="{ 'ds-fab-segment__btn--active': activeMode === 'knowledge' }"
      @click="emit('update:mode', 'knowledge')"
    >
      知识库
    </button>
  </div>
</template>
