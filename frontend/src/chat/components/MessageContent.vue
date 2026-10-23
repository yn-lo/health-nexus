<script setup lang="ts">
import { computed } from 'vue'
import MarkdownIt from 'markdown-it'
import { sanitizeHtml } from '@/shared/utils/sanitize-html'
import { citedRefs } from '@/shared/utils/citations'
import type { Reference } from '@/shared/types/chat'

const props = withDefaults(defineProps<{
  content: string
  references?: Reference[]
  streaming?: boolean
}>(), {
  references: () => [],
  streaming: false,
})

const emit = defineEmits<{ cite: [articleId: number] }>()

const md = new MarkdownIt({ html: false, breaks: true, linkify: true })
/** 引用标记 `[资料N]`：N 对应"参考来源"（按 article_id 去重后）第 N 篇文章 */
const CITE_RE = /\[资料\s*(\d+)\]/g
/** 文章 ID 属性白名单（仅字母/数字/_/-），避免拼接进 HTML 属性时被注入 */
const SAFE_ARTICLE_ID = /^[A-Za-z0-9_-]+$/

/**
 * 渲染 markdown 为 HTML（经白名单消毒，防 XSS），并把 `[资料N]` 美化为可点击角标。
 * 角标在消毒**之后**注入：消毒器会剥掉 class，故样式化只能发生在此之后；
 * 注入内容仅数字编号与经白名单校验的文章 ID，无注入面。
 */
function render(text: string, refs: Reference[]): string {
  const html = sanitizeHtml(md.render(text))
  if (!html.includes('[资料')) return html
  // 引用单位 = 文章：正文 [资料N] 的 N 指向本轮 references 第 N 条（后端已按文章归并）。
  // 角标序号 = 该文章在"被引用文章"列表中的位置（1 起），与下方卡片序号一致；
  // 未被正文引用的候选文章不展示，故角标可能被重新编号（如 [资料3] → 1）。
  const cited = citedRefs(text, refs)
  const posByArticle = new Map<number, number>()
  cited.forEach((r, i) => posByArticle.set(r.article_id, i + 1))
  return html.replace(CITE_RE, (_m, n: string) => {
    const raw = refs[Number(n) - 1]?.article_id
    if (raw !== undefined) {
      const article = String(raw)
      if (SAFE_ARTICLE_ID.test(article)) {
        const label = posByArticle.get(raw) ?? n
        return `<sup class="cite-chip" data-cite-article="${article}" role="button">${label}</sup>`
      }
    }
    return `<sup class="cite-chip cite-chip--plain">${n}</sup>`
  })
}

const html = computed(() =>
  render(props.content, props.references) + (props.streaming ? '<span class="streaming-cursor"></span>' : ''),
)

function onClick(e: MouseEvent) {
  const chip = (e.target as HTMLElement | null)?.closest('[data-cite-article]') as HTMLElement | null
  const articleId = chip?.dataset.citeArticle
  if (articleId) emit('cite', Number(articleId))
}
</script>

<template>
  <div class="m-0 break-words markdown-content" v-html="html" @click="onClick"></div>
</template>
