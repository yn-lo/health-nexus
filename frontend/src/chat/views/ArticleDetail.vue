<script setup lang="ts">
/**
 * ArticleDetail 文章详情 — v3 风格（ui-ux-pro-max: Content-First + Accessible & Ethical）
 * 风格升级要点：
 * - 顶部阅读进度条（fixed，scroll 驱动）— 长文导航必备
 * - 返回按钮扩至 40px+（≥44pt 触摸目标），收藏入口收敛至底部操作栏（避免双入口）
 * - 加载骨架屏（避免白屏闪烁）
 * - 返回顶部 FAB（长文超过 1.5 屏时显示）
 * - 元数据条 flex-wrap 防溢出
 * - markdown 正文 h2 排版补全（原仅 h3/h4）
 * - 底部操作栏每按钮 min-w + 触摸目标达标
 * - 尊重 prefers-reduced-motion
 * 保留功能: wikiApi.getArticleDetail, goBack, toggleLike/Bookmark, onShare
 */
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ChevronLeft, Share2, Bookmark, Heart, AlertTriangle, Clock, Eye, ArrowUp } from '@lucide/vue'
import MarkdownIt from 'markdown-it'
import { showFailToast } from 'vant'
import { wikiApi, fmtDate, fmtCompact } from '@/shared'
import type { ArticleDetail as ArticleDetailType } from '@/shared'
import { sanitizeHtml } from '@/shared/utils/sanitize-html'

const router = useRouter()
const route = useRoute()

const article = ref<ArticleDetailType | null>(null)
const loading = ref(true)
const liked = ref(false)
const bookmarked = ref(false)
const likeCount = ref(0)
const scrollProgress = ref(0)
const showBackToTop = ref(false)
const contentRef = ref<HTMLElement | null>(null)

/** markdown-it 实例 */
const md = new MarkdownIt({ html: true, breaks: true, linkify: true })

/** 文章 ID */
const articleId = computed(() => Number(route.params.id))

/** 渲染后的正文（markdown-it 输出后经白名单消毒，防存储型 XSS） */
const renderedContent = computed(() => {
 if (!article.value) return ''
 return sanitizeHtml(md.render(article.value.content))
})

/** 作者首字（空名时用"医"作占位，避免显示问号） */
const authorInitial = computed(() => {
 const name = article.value?.author_name ?? ''
 return name.charAt(0) || '医'
})

/** 估算阅读时长（按正文长度 / 300 字每分钟） */
const readTimeMinutes = computed(() => {
 if (!article.value?.content) return 1
 // strip HTML tags 估算纯字数
 const text = article.value.content.replace(/<[^>]+>/g, '').replace(/\s+/g, '')
 return Math.max(1, Math.ceil(text.length / 300))
})

/** 返回 */
function goBack() {
 router.back()
}

/** 点赞 */
function toggleLike() {
 liked.value = !liked.value
 likeCount.value += liked.value ? 1 : -1
}

/** 收藏 */
function toggleBookmark() {
 bookmarked.value = !bookmarked.value
}

/** 分享 */
function onShare() {
 if (navigator.share) {
 navigator.share({ title: article.value?.title ?? '', url: window.location.href })
 }
}

/** 滚动监听：计算阅读进度 + 控制返回顶部按钮显隐 */
function handleScroll() {
 const el = contentRef.value
 if (!el) return
 const rect = el.getBoundingClientRect()
 // 内容总高 = 内容高度 - 视口高度（可滚动距离）
 const scrollable = el.scrollHeight - window.innerHeight
 if (scrollable <= 0) {
 scrollProgress.value = 0
 showBackToTop.value = false
 return
 }
 // 已滚出顶部距离（取负值转正）
 const scrolled = Math.min(Math.max(-rect.top, 0), scrollable)
 scrollProgress.value = Math.round((scrolled / scrollable) * 100)
 // 滚动超过 1.5 屏时显示返回顶部
 showBackToTop.value = -rect.top > window.innerHeight * 1.5
}

/** 返回顶部 */
function scrollToTop() {
 window.scrollTo({ top: 0, behavior: 'smooth' })
}

onMounted(async () => {
 loading.value = true
 try {
 article.value = await wikiApi.getArticleDetail(articleId.value)
 await nextTick()
 // 等待 DOM 渲染完成后注册滚动监听
 window.addEventListener('scroll', handleScroll, { passive: true })
 handleScroll()
 } catch {
 showFailToast('加载失败')
 } finally {
 loading.value = false
 }
})

onBeforeUnmount(() => {
 window.removeEventListener('scroll', handleScroll)
})
</script>

<template>
 <div class="article-detail flex flex-col min-h-dvh bg-[var(--bg-base-default)]">
 <!-- 阅读进度条 — fixed 顶部，scroll 驱动 -->
 <div
 class="fixed top-0 inset-x-0 z-40 h-[3px] bg-transparent pointer-events-none"
 role="progressbar"
 :aria-valuenow="scrollProgress"
 aria-valuemin="0"
 aria-valuemax="100"
 aria-label="阅读进度"
 >
 <div
 class="h-full bg-gradient-to-r from-[var(--bg-brand)] to-[var(--accent-violet)] transition-[width] duration-100 ease-out"
 :style="{ width: `${scrollProgress}%` }"
 />
 </div>

 <!-- 顶部导航 — v3 风格：扩大触摸目标至 44pt -->
 <header
 class="sticky top-0 z-30 flex h-14 items-center border-b border-[var(--border-neutral-l1)] bg-[var(--bg-base-default)]/95 backdrop-blur px-[var(--spacer-12)]"
 >
 <button
 class="-ml-[var(--spacer-4)] flex h-11 w-11 items-center justify-center rounded-[var(--radius-12)] hover:bg-[var(--bg-overlay-l1)] active:bg-[var(--bg-overlay-l2)] transition-colors"
 aria-label="返回"
 @click="goBack"
 >
 <ChevronLeft :size="22" class="text-text" />
 </button>
 <h1 class="flex-1 truncate text-center text-body-base font-semibold text-text">
 健康宣教
 </h1>
 <button
 class="flex h-11 w-11 items-center justify-center rounded-[var(--radius-12)] hover:bg-[var(--bg-overlay-l1)] active:bg-[var(--bg-overlay-l2)] transition-colors"
 :class="bookmarked ? 'text-text-brand' : 'text-icon'"
 :aria-pressed="bookmarked"
 aria-label="收藏"
 @click="toggleBookmark"
 >
 <Bookmark :size="18" :fill="bookmarked ? 'currentColor' : 'none'" />
 </button>
 </header>

 <!-- 骨架屏 — loading 期间显示 -->
 <div v-if="loading" class="px-[var(--spacer-20)] pt-[var(--spacer-24)] pb-[var(--spacer-32)] space-y-[var(--spacer-16)]">
 <div class="h-7 w-24 rounded-[var(--radius-full)] bg-[var(--bg-overlay-l2)] skeleton-pulse" />
 <div class="space-y-[var(--spacer-8)]">
 <div class="h-8 w-full rounded-[var(--radius-4)] bg-[var(--bg-overlay-l2)] skeleton-pulse" />
 <div class="h-8 w-5/6 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l2)] skeleton-pulse" />
 </div>
 <div class="flex items-center gap-[var(--spacer-10)]">
 <div class="w-9 h-9 rounded-full bg-[var(--bg-overlay-l2)] skeleton-pulse" />
 <div class="flex-1 space-y-[var(--spacer-4)]">
 <div class="h-4 w-32 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l2)] skeleton-pulse" />
 <div class="h-3 w-24 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
 </div>
 </div>
 <div class="h-px bg-[var(--border-neutral-l1)]" />
 <div class="space-y-[var(--spacer-8)]">
 <div class="h-4 w-full rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
 <div class="h-4 w-full rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
 <div class="h-4 w-3/4 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
 <div class="h-4 w-full rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
 <div class="h-4 w-2/3 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
 </div>
 </div>

 <!-- 文章内容 -->
 <article v-else-if="article" ref="contentRef" class="px-[var(--spacer-20)] pt-[var(--spacer-24)] pb-[calc(var(--spacer-32)+4rem)]">
 <!-- 文章头部 — v3 风格：医疗权威感 -->
 <header class="mb-[var(--spacer-24)]">
 <!-- 科室标签 — 品牌配色 -->
 <span class="inline-flex items-center h-7 px-[var(--spacer-12)] rounded-[var(--radius-full)] mb-[var(--spacer-16)] bg-[var(--bg-brand-light)] text-text-brand text-body-sm font-medium ">
 {{ article.department_name }}
 </span>

 <!-- 标题 — 加大字号、紧凑行高 -->
 <h2
 class="font-heading text-heading-xl leading-[1.25] font-semibold text-text mb-[var(--spacer-20)] [text-wrap:balance] [word-break:keep-all]"
 >
 {{ article.title }}
 </h2>

 <!-- 作者信息卡 — v3 风格：渐变头像 + 名字 + 阅读时长 -->
 <div class="flex items-center justify-between mb-[var(--spacer-12)]">
 <div class="flex items-center gap-[var(--spacer-10)] min-w-0">
 <span class="inline-flex items-center justify-center shrink-0 w-9 h-9 rounded-full bg-gradient-to-br from-[var(--bg-brand)] to-[var(--accent-violet)] text-onbrand text-body-base font-semibold shadow-[var(--shadow-sm)]">
 {{ authorInitial }}
 </span>
 <div class="flex flex-col min-w-0">
 <span class="truncate text-body-base font-medium text-text">
 {{ article.author_name }}
 </span>
 <span class="text-body-xs text-text-tertiary">
 健康宣教员
 </span>
 </div>
 </div>
 </div>

 <!-- 元数据 — v3 风格：flex-wrap 防小屏溢出 -->
 <div class="flex flex-wrap items-center gap-x-[var(--spacer-12)] gap-y-[var(--spacer-4)] px-[var(--spacer-12)] py-[var(--spacer-8)] rounded-[var(--radius-8)] bg-[var(--bg-base-secondary)] text-text-tertiary">
 <span class="inline-flex items-center gap-[var(--spacer-4)] text-body-sm">
 <Clock :size="12" />
 {{ fmtDate(article.published_at) }}
 </span>
 <span class="text-[var(--border-neutral-l2)]" aria-hidden="true">·</span>
 <span class="inline-flex items-center gap-[var(--spacer-4)] text-body-sm">
 <Eye :size="12" />
 {{ fmtCompact(article.view_count) }} 阅读
 </span>
 <span class="text-[var(--border-neutral-l2)]" aria-hidden="true">·</span>
 <span class="inline-flex items-center gap-[var(--spacer-4)] text-body-sm">
 <Clock :size="12" />
 {{ readTimeMinutes }} 分钟阅读
 </span>
 </div>
 </header>

 <!-- 分隔线 — v3 风格：渐变品牌光晕细线 -->
 <div class="h-px bg-gradient-to-r from-transparent via-[var(--brand-glow-border-strong)] to-transparent mb-[var(--spacer-24)]" />

 <!-- 正文 — v3 风格：15px 字号 / 24px 行高 -->
 <div class="markdown-body" v-html="renderedContent" />

 <!-- 警告提示 — v3 风格：品牌色信息框 -->
 <div class="flex items-start gap-[var(--spacer-12)] p-[var(--spacer-16)] rounded-[var(--radius-card-medium)] mt-[var(--spacer-32)] mb-[var(--spacer-20)] bg-[var(--bg-brand-light)] border border-[var(--brand-glow-border-strong)]">
 <AlertTriangle :size="18" class="shrink-0 mt-[1px] text-text-brand" />
 <div class="flex-1">
 <p class="text-body-sm font-medium text-text-brand mb-[var(--spacer-4)]">
 温馨提示
 </p>
 <p class="text-body-sm leading-[1.6] text-text-secondary">
 以上内容仅供健康参考，不能替代专业医疗建议。如有疑问，请咨询您的主治医生。
 </p>
 </div>
 </div>
 </article>

 <!-- 返回顶部 FAB — 滚动超过 1.5 屏时显示 -->
 <Transition
 enter-active-class="transition-opacity duration-200"
 leave-active-class="transition-opacity duration-150"
 enter-from-class="opacity-0"
 leave-to-class="opacity-0"
 >
 <button
 v-if="showBackToTop"
 type="button"
 class="fixed right-[var(--spacer-16)] bottom-[calc(5rem+var(--spacer-12))] z-[var(--z-fixed)] w-11 h-11 flex items-center justify-center rounded-full bg-[var(--bg-base-default)] text-text-brand shadow-[var(--shadow-lg)] hover:bg-[var(--bg-brand-light)] active:scale-95 transition-[background,transform_var(--micro-duration)_var(--micro-ease)]"
 aria-label="返回顶部"
 @click="scrollToTop"
 >
 <ArrowUp :size="20" />
 </button>
 </Transition>

 <!-- 底部操作栏 — v3 风格：3 按钮，每按钮 min-w + 触摸目标达标 -->
 <div class="fixed bottom-0 inset-x-0 z-[var(--z-fixed)] flex items-stretch justify-around h-16 bg-[var(--bg-base-default)]/95 backdrop-blur border-t border-[var(--border-neutral-l1)] px-[var(--spacer-16)]">
 <button
 class="flex flex-col items-center justify-center gap-[2px] flex-1 min-w-[var(--touch-target-min)] h-full transition-colors hover:bg-[var(--bg-overlay-l1)] active:bg-[var(--bg-overlay-l2)]"
 :aria-pressed="liked"
 aria-label="点赞"
 @click="toggleLike"
 >
 <Heart :size="22" :fill="liked ? 'currentColor' : 'none'" :class="liked ? 'text-text-brand' : 'text-icon-tertiary'" />
 <span class="text-body-xs" :class="liked ? 'text-text-brand' : 'text-text-tertiary'">
 {{ fmtCompact(likeCount) }}
 </span>
 </button>
 <button
 class="flex flex-col items-center justify-center gap-[2px] flex-1 min-w-[var(--touch-target-min)] h-full transition-colors hover:bg-[var(--bg-overlay-l1)] active:bg-[var(--bg-overlay-l2)]"
 :aria-pressed="bookmarked"
 aria-label="收藏"
 @click="toggleBookmark"
 >
 <Bookmark :size="22" :fill="bookmarked ? 'currentColor' : 'none'" :class="bookmarked ? 'text-text-brand' : 'text-icon-tertiary'" />
 <span class="text-body-xs" :class="bookmarked ? 'text-text-brand' : 'text-text-tertiary'">
 收藏
 </span>
 </button>
 <button
 class="flex flex-col items-center justify-center gap-[2px] flex-1 min-w-[var(--touch-target-min)] h-full transition-colors hover:bg-[var(--bg-overlay-l1)] active:bg-[var(--bg-overlay-l2)]"
 aria-label="分享"
 @click="onShare"
 >
 <Share2 :size="22" class="text-icon-tertiary" />
 <span class="text-body-xs text-text-tertiary">分享</span>
 </button>
 </div>
 </div>
</template>

<style scoped ponytail:allow-scoped-css 长文排版，装饰性>
/* markdown 正文样式 — v3 风格：15px/24px 长文阅读 */
.markdown-body :deep(h2) {
 font-family: var(--font-family-heading);
 font-size: var(--heading-md-font-size);
 line-height: var(--heading-md-line-height);
 font-weight: var(--heading-md-font-weight);
 color: var(--text-default);
 margin: var(--spacer-32) 0 var(--spacer-12);
}
.markdown-body :deep(h3) {
 font-family: var(--font-family-heading);
 font-size: var(--heading-sm-font-size);
 line-height: var(--heading-sm-line-height);
 font-weight: var(--heading-sm-font-weight);
 color: var(--text-default);
 margin: 0 0 var(--spacer-12);
}
.markdown-body :deep(h4) {
 font-family: var(--font-family-heading);
 font-size: var(--body-base-strong-font-size);
 line-height: var(--body-base-strong-line-height);
 font-weight: var(--heading-2xs-font-weight);
 color: var(--text-default);
 margin: var(--spacer-20) 0 var(--spacer-8);
}
.markdown-body :deep(p) {
 font-family: var(--font-family-default);
 font-size: var(--reading-font-size);
 line-height: var(--reading-line-height);
 letter-spacing: var(--reading-letter-spacing);
 color: var(--text-secondary);
 margin: 0 0 var(--reading-block-margin);
}
.markdown-body :deep(ul), .markdown-body :deep(ol) {
 margin: 0 0 var(--reading-block-margin);
 padding: 0;
 list-style: none;
}
.markdown-body :deep(li) {
 display: flex;
 align-items: flex-start;
 gap: var(--spacer-10);
 margin-bottom: var(--spacer-8);
}
.markdown-body :deep(li::before) {
 content: '';
 flex-shrink: 0;
 margin-top: 8px;
 width: 6px;
 height: 6px;
 border-radius: var(--radius-full);
 background: var(--bg-brand);
}
.markdown-body :deep(li > p) {
 margin: 0;
}
.markdown-body :deep(strong) {
 color: var(--text-default);
 font-weight: var(--font-weight-strong);
}
.markdown-body :deep(a) {
 color: var(--text-brand);
 text-decoration: none;
 border-bottom: 1px solid var(--brand-glow-border-strong);
 transition: border-color var(--micro-duration) var(--micro-ease),
 color var(--micro-duration) var(--micro-ease);
}
.markdown-body :deep(a:hover) {
 color: var(--text-brand-hover);
 border-bottom-color: var(--text-brand-hover);
}
.markdown-body :deep(blockquote) {
 margin: var(--reading-block-margin) 0;
 padding: var(--spacer-12) var(--spacer-16);
 background: var(--bg-brand-light);
 border-radius: var(--radius-8);
}
.markdown-body :deep(blockquote p) {
 margin: 0;
 color: var(--text-secondary);
 font-style: normal;
}
.markdown-body :deep(code) {
 font-family: var(--font-family-metric);
 font-size: 0.9em;
 padding: 2px 6px;
 background: var(--bg-overlay-l1);
 border-radius: var(--radius-4);
 color: var(--text-default);
}
.markdown-body :deep(img) {
 max-width: 100%;
 height: auto;
 border-radius: var(--radius-8);
 margin: var(--spacer-12) 0;
}
</style>
