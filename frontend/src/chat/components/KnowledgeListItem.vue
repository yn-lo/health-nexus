<script setup lang="ts">
/**
 * KnowledgeListItem — 知识库文章列表项（热门置顶 / 全部文章共用）
 * 图标 + 标题 + 元信息（科室/日期/阅读时长/浏览量）+ 可展开摘要
 */
import type { Component } from 'vue'
import { Calendar, Clock, Eye, ChevronDown } from '@lucide/vue'
import MarkdownIt from 'markdown-it'
import { fmtCompact, fmtShortDate } from '@/shared'
import type { ArticlePublic } from '@/shared'

withDefaults(defineProps<{
  article: ArticlePublic
  icon: Component
  expanded: boolean
  /** 图标底色：brand=普通文章，alert=置顶文章（仅颜色区分，不新增区块） */
  tone?: 'brand' | 'alert'
}>(), {
  tone: 'brand',
})
const emit = defineEmits<{
  (e: 'open', id: number): void
  (e: 'toggle', id: number): void
}>()

/** markdown-it 实例（html:false 防止 summary 内嵌 HTML 被渲染，breaks:true 保留换行） */
const md = new MarkdownIt({ html: false, breaks: true, linkify: true })

/** Markdown -> 纯文本：渲染成 HTML 后，在块级标签边界插入空格再 strip，避免表格/列表粘连 */
function renderPlainText(markdown: string): string {
  if (!markdown) return ''
  return md.render(markdown)
    .replace(/<\/(p|div|h[1-6]|li|tr|td|th|br)\s*>/gi, ' ')
    .replace(/<[^>]*>/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}

/** 估算阅读时长（按摘要字数 / 300 字每分钟） */
function estimateReadTime(summary: string): number {
  if (!summary) return 1
  return Math.max(1, Math.ceil(summary.length / 300))
}
</script>

<template>
  <div class="ds-list-item min-h-[var(--touch-target-min)]">
    <span class="ds-list-item__icon" :class="`ds-list-item__icon--${tone}`">
      <component :is="icon" :size="20" />
    </span>
    <div class="ds-list-item__content" @click="emit('open', article.id)">
      <span class="ds-list-item__title">{{ article.title }}</span>
      <span class="ds-list-item__meta">
        <span class="truncate">{{ article.department_name }}</span>
        <span class="text-[var(--border-neutral-l2)]" aria-hidden="true">·</span>
        <span class="inline-flex items-center gap-[var(--spacer-2)] shrink-0">
          <Calendar :size="11" />
          {{ article.published_at ? fmtShortDate(article.published_at) : '-' }}
        </span>
        <span class="text-[var(--border-neutral-l2)]" aria-hidden="true">·</span>
        <span class="inline-flex items-center gap-[var(--spacer-2)] shrink-0">
          <Clock :size="11" />
          {{ estimateReadTime(article.summary) }}分
        </span>
        <span class="text-[var(--border-neutral-l2)]" aria-hidden="true">·</span>
        <span class="inline-flex items-center gap-[var(--spacer-2)] shrink-0">
          <Eye :size="11" />
          {{ fmtCompact(article.view_count) }}
        </span>
      </span>
    </div>
    <button
      type="button"
      class="ds-list-item__trailing justify-center w-9 h-9 rounded-[var(--radius-8)] text-icon-tertiary hover:bg-[var(--bg-overlay-l1)] active:bg-[var(--bg-overlay-l2)] transition-[background-color_var(--micro-duration)_var(--micro-ease)]"
      :aria-label="expanded ? '收起摘要' : '展开摘要'"
      :aria-expanded="expanded"
      @click.stop="emit('toggle', article.id)"
    >
      <ChevronDown
        :size="18"
        class="transition-transform duration-200"
        :class="expanded ? 'rotate-180' : ''"
      />
    </button>
  </div>
  <div
    v-if="expanded"
    class="px-[var(--spacer-16)] pb-[var(--spacer-12)] text-body-sm leading-[1.6] text-text-secondary"
  >
    {{ renderPlainText(article.summary) }}
  </div>
</template>
