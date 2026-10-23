<script setup lang="ts">
import { computed } from 'vue'
import { citedRefs } from '@/shared/utils/citations'
import type { Reference } from '@/shared/types/chat'

const props = withDefaults(defineProps<{
  content: string
  references?: Reference[]
}>(), {
  references: () => [],
})

const emit = defineEmits<{ open: [articleId: number] }>()

/** 仅展示正文实际引用的文章；未引用的候选不展示 */
const items = computed(() => citedRefs(props.content, props.references))
</script>

<template>
  <div v-if="items.length" class="mt-[var(--spacer-12)]">
    <p class="text-body-xs text-text-tertiary m-0 mb-[var(--spacer-6)]">参考来源</p>
    <div class="flex flex-col gap-[var(--spacer-6)]">
      <div
        v-for="(ref, idx) in items"
        :key="ref.article_id"
        :data-article-id="String(ref.article_id)"
        class="ref-card group flex items-center gap-[var(--spacer-10)] rounded-[var(--radius-12)] bg-[var(--bg-overlay-l1)] px-[var(--spacer-12)] py-[var(--spacer-8)] ring-1 ring-[var(--border-neutral-l1)] transition-all active:scale-[0.98]"
        @click="emit('open', ref.article_id)"
      >
        <span class="ref-card__idx">{{ idx + 1 }}</span>
        <p class="flex-1 min-w-0 text-body-sm text-text-secondary m-0 leading-snug line-clamp-2">
          {{ ref.article_title }}
        </p>
      </div>
    </div>
  </div>
</template>
