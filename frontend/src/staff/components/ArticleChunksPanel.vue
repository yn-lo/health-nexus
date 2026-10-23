<script setup lang="ts">
import { computed, ref } from 'vue'
import { ChevronDown, Layers, RefreshCw } from '@lucide/vue'
import { stripHtml, fmtDateTime } from '@/shared'
import type { ArticleChunk } from '@/shared'

const props = defineProps<{
  chunks: ArticleChunk[]
  loading: boolean
  revectorizing: boolean
}>()

const emit = defineEmits<{ revectorize: [] }>()

const expanded = ref(false)
const expandedChunkId = ref<number | null>(null)

/** 切片最后生成时间（取首片 created_at 作为代理） */
const createdAt = computed(() => props.chunks[0]?.created_at ?? '')
</script>

<template>
  <div class="flex flex-col gap-[var(--spacer-8)]">
    <div class="flex items-center justify-between">
      <label class="font-heading text-body-base font-medium text-text">
        切片状态
      </label>
      <button
        type="button"
        class="inline-flex items-center gap-[var(--spacer-4)] border-none bg-transparent font-heading text-body-sm text-text-brand disabled:opacity-50"
        :disabled="revectorizing"
        @click="emit('revectorize')"
      >
        <RefreshCw class="h-3.5 w-3.5" :class="revectorizing ? 'animate-spin' : ''" />
        重新切片
      </button>
    </div>
    <div
      class="rounded-[var(--ds-control-radius-md)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] px-[var(--spacer-12)] py-[var(--spacer-12)]"
    >
      <!-- 概要 -->
      <div class="flex items-center gap-[var(--spacer-8)]">
        <span class="inline-flex h-7 w-7 items-center justify-center rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] text-icon">
          <Layers class="h-4 w-4" />
        </span>
        <div class="flex-1 flex flex-col gap-[var(--spacer-2)]">
          <span v-if="loading" class="font-heading text-body-sm text-text-tertiary">加载中…</span>
          <span v-else-if="chunks.length === 0" class="font-heading text-body-sm text-[var(--status-error-default)]">
            暂无切片（向量化可能失败，可尝试重新切片）
          </span>
          <span v-else class="font-heading text-body-sm text-text">
            共 {{ chunks.length }} 片 · 版本 v{{ chunks[0]?.version }}
          </span>
          <span v-if="createdAt" class="font-heading text-body-xs text-text-tertiary">
            生成于 {{ fmtDateTime(createdAt) }}
          </span>
        </div>
        <button
          v-if="chunks.length > 0"
          type="button"
          class="inline-flex h-7 w-7 items-center justify-center rounded-[var(--radius-4)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-default)] text-icon transition-colors hover:bg-[var(--bg-overlay-l1)]"
          :aria-label="expanded ? '收起' : '展开'"
          @click="expanded = !expanded"
        >
          <ChevronDown
            class="h-4 w-4 transition-transform"
            :class="expanded ? 'rotate-180' : ''"
          />
        </button>
      </div>
      <!-- 切片详情列表 -->
      <div
        v-if="expanded && chunks.length > 0"
        class="mt-[var(--spacer-12)] flex flex-col gap-[var(--spacer-8)] border-t border-[var(--border-neutral-l1)] pt-[var(--spacer-12)]"
      >
        <div
          v-for="chunk in chunks"
          :key="chunk.id"
          class="flex cursor-pointer flex-col gap-[var(--spacer-2)] rounded-[var(--radius-4)] px-[var(--spacer-4)] py-[var(--spacer-4)] transition-colors hover:bg-[var(--bg-overlay-l1)]"
          @click="expandedChunkId = expandedChunkId === chunk.id ? null : chunk.id"
        >
          <div class="flex items-center gap-[var(--spacer-4)]">
            <span class="inline-flex h-5 min-w-5 items-center justify-center rounded-[var(--radius-2)] bg-[var(--bg-brand)] px-[var(--spacer-4)] font-heading text-body-xs font-medium text-onbrand">
              {{ chunk.chunk_index + 1 }}
            </span>
            <span class="font-heading text-body-xs text-text-tertiary">
              {{ chunk.content_hash.slice(0, 8) }}
            </span>
          </div>
          <p
            class="font-heading text-body-sm text-text-secondary"
            :class="expandedChunkId === chunk.id ? '' : 'line-clamp-2'"
          >
            {{ stripHtml(chunk.content) }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>
