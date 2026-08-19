<script setup lang="ts">
/**
 * 医护端文章管理列表 - v4 风格（mobile-app-ui-design skill）
 * 设计升级要点（对照 skill 原则）：
 * - Hero 统计卡：品牌渐变软底，创造视觉锤（Peak-End / Visual Hierarchy）
 * - 状态色图标：published=success / pending=brand，一眼可扫（60/30/10 色彩规则）
 * - 标签页计数徽章：无需切换即可知分布（Smarter Patterns）
 * - 块状卡片（ds-list-item--block）：标题两行截断 + 摘要预览 + meta 整行舒展，
 * 状态标签居卡片右上角，操作按钮下沉到分隔线 footer，消除单行列表的横向挤压
 * - 浏览量展示：published 文章显示 view_count（Expose content directly）
 * - 增强空状态：图标 + 引导文案 + CTA（Anti-Pattern: Generic empty states）
 * - 精简 meta：去掉冗余 author（listMyArticles 即本人），改为科室·日期·浏览量
 * API: wikiApi.listMyArticles() + wikiApi.deleteArticle()
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Trash2, Search, FileText, ChevronRight, Archive, ArchiveRestore, Eye, PenLine, Star } from '@lucide/vue'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { AppHeader, StatRow } from '@/shared/components'
import { wikiApi, fmtShortDate, fmtCompact, stripHtml } from '@/shared'
import { errmsg } from '@/shared/api/client'
import type { ArticleStaff, ArticleStatus } from '@/shared'
import { useAuthStore } from '@/stores/auth'
import { ADMIN_ROLES } from '@/shared/constants/roles'

const router = useRouter()
const authStore = useAuthStore()
const { showSuccessToast, showFailToast } = useDsToast()
const { showConfirmDialog } = useDsDialog()

const articles = ref<ArticleStaff[]>([])
const loading = ref(false)
const searchQuery = ref('')
const activeTab = ref('all')
const canManageFeatured = computed(() => Boolean(authStore.user && ADMIN_ROLES.includes(authStore.user.role)))

/** 筛选标签页（对照设计稿: 全部/已发布/待审核/草稿） */
const tabItems = [
 { key: 'all', label: '全部' },
 { key: 'published', label: '已发布' },
 { key: 'pending', label: '待审核' },
 { key: 'draft', label: '草稿' },
 { key: 'archived', label: '已归档' },
]

/** 状态 -> 文案 */
interface StatusConfig {
 label: string
}

const statusConfig: Record<ArticleStatus, StatusConfig> = {
 published: { label: '已发布' },
 pending: { label: '待审核' },
 draft: { label: '草稿' },
 archived: { label: '已归档' },
 deleted: { label: '已删除' },
}

/** 状态对应的 ds-tag 变体（红黄仅留给错误/破坏性场景，工作流状态用品牌色） */
const statusTagType: Record<ArticleStatus, 'success' | 'primary' | 'default'> = {
 published: 'success',
 pending: 'primary',
 draft: 'default',
 archived: 'default',
 deleted: 'default',
}

/** 状态 -> 列表图标色变体（视觉层级：published 绿 / pending 蓝 / 其余中性） */
const statusIconVariant: Record<ArticleStatus, string> = {
 published: 'ds-list-item__icon--success',
 pending: 'ds-list-item__icon--brand',
 draft: '',
 archived: '',
 deleted: '',
}

/** 标签页计数 - 无需切换即可知各状态分布 */
const statusCounts = computed<Record<string, number>>(() => {
 const counts: Record<string, number> = { all: articles.value.length }
 for (const s of ['published', 'pending', 'draft', 'archived'] as ArticleStatus[]) {
 counts[s] = articles.value.filter((a) => a.status === s).length
 }
 return counts
})

/** 按状态返回 meta 文案：已发布显示发布日期，其余显示最近更新时间（状态标签已在卡片右上角，meta 不再重复） */
function metaText(article: ArticleStaff): string {
 if (article.status === 'published' && article.published_at) {
 return `发布于 ${fmtShortDate(article.published_at)}`
 }
 return `更新于 ${fmtShortDate(article.updated_at)}`
}

/** 骨架占位条数 */
const skeletonRows = [0, 1, 2]

/** 统计数据 */
const totalCount = computed(() => articles.value.length)
const publishedCount = computed(() => articles.value.filter((a) => a.status === 'published').length)
const pendingCount = computed(() => articles.value.filter((a) => a.status === 'pending').length)

/** 过滤后的文章列表 */
const filteredArticles = computed(() => {
 let list = articles.value
 if (activeTab.value !== 'all') {
 list = list.filter((a) => a.status === activeTab.value)
 }
 if (searchQuery.value.trim()) {
 const q = searchQuery.value.trim().toLowerCase()
 list = list.filter(
 (a) =>
 a.title.toLowerCase().includes(q) ||
 a.author_name.toLowerCase().includes(q) ||
 a.department_name.toLowerCase().includes(q),
 )
 }
 return list
})

/** 新建文章 */
function goCreate() {
 router.push({ name: 'staff-article-create' })
}

/** 编辑文章 */
function goEdit(id: number) {
 router.push({ name: 'staff-article-edit', params: { id } })
}

/** 删除文章（带确认弹窗） */
async function handleDelete(id: number) {
 try {
 await showConfirmDialog({
 title: '确认删除',
 message: '删除后无法恢复，确定要删除这篇文章吗？',
 confirmButtonText: '删除',
 danger: true,
 cancelButtonText: '取消',
 })
 } catch {
 return
 }
 try {
 await wikiApi.deleteArticle(id)
 articles.value = articles.value.filter((a) => a.id !== id)
 showSuccessToast('已删除')
 } catch (e) {
 showFailToast(errmsg(e, '删除失败'))
 }
}

/** 归档文章（published → archived） */
async function handleArchive(id: number) {
 try {
 await showConfirmDialog({
 title: '确认归档',
 message: '归档后文章将不再对外展示，确定归档吗？',
 confirmButtonText: '归档',
 cancelButtonText: '取消',
 })
 } catch {
 return
 }
 try {
 await wikiApi.archiveArticle(id)
 const target = articles.value.find((a) => a.id === id)
 if (target) target.status = 'archived'
 showSuccessToast('已归档')
 } catch (e) {
 showFailToast(errmsg(e, '归档失败'))
 }
}

/** 取消归档（archived → published） */
async function handleUnarchive(id: number) {
 try {
 await wikiApi.unarchiveArticle(id)
 const target = articles.value.find((a) => a.id === id)
 if (target) target.status = 'published'
 showSuccessToast('已取消归档')
 } catch (e) {
 showFailToast(errmsg(e, '取消归档失败'))
 }
}

async function handleFeatured(article: ArticleStaff, rank: number) {
 try {
 await wikiApi.setArticleFeatured(article.id, rank)
 if (rank > 0) {
 for (const item of articles.value) {
 if (item.department_id === article.department_id && item.featured_rank === rank) item.featured_rank = 0
 }
 }
 article.featured_rank = rank
 showSuccessToast(rank ? `已设为热门 ${rank}` : '已取消热门')
 } catch (e) {
 showFailToast(errmsg(e, '设置热门失败'))
 }
}

onMounted(async () => {
 loading.value = true
 try {
 // ponytail: 拉取较大 page_size 以一次性加载本地过滤，折中；如文章数增长可改服务端分页 + tab 切换拉取。
 const res = await wikiApi.listMyArticles({ page: 1, page_size: 100 })
 articles.value = res.items
 } catch (e) {
 showFailToast(errmsg(e, '加载失败'))
 } finally {
 loading.value = false
 }
})
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="文章管理" @back="router.back">
 <template #right>
 <button
 type="button"
 class="ds-icon-btn ds-icon-btn--sm ds-icon-btn--brand"
 aria-label="新建文章"
 @click="goCreate"
 >
 <Plus class="icon h-5 w-5" />
 </button>
 </template>
 </AppHeader>

 <!-- Hero 统计卡 - 品牌渐变软底，创造视觉锤 -->
 <section class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <div class="ds-card p-[var(--spacer-20)] bg-[var(--ai-gradient-soft)]">
 <StatRow
 :stats="[
 { value: totalCount, label: '总文章' },
 { value: publishedCount, label: '已发布' },
 { value: pendingCount, label: '待审核' },
 ]"
 />
 </div>
 </section>

 <!-- 搜索栏 - v3 风格：硬编码 rgba 改用 token，扩大触摸高度 -->
 <section class="px-[var(--spacer-16)] pb-[var(--spacer-12)]">
 <div class="ds-search-box ds-search-box--md">
 <Search class="h-4 w-4 shrink-0 text-icon-brand" />
 <input
 v-model="searchQuery"
 type="text"
 inputmode="search"
 placeholder="搜索文章标题..."
 class="min-w-0 flex-1 border-none bg-transparent font-heading text-body-base text-text outline-none placeholder:text-text-tertiary"
 >
 </div>
 </section>

 <!-- 筛选标签页 - v4 风格：计数徽章（新建入口统一在 AppHeader 右侧） -->
 <section class="px-[var(--spacer-16)]">
  <select v-model="activeTab" class="ds-input">
   <option value="all">全部状态</option>
   <option v-for="tab in tabItems.slice(1)" :key="tab.key" :value="tab.key">{{ tab.label }}（{{ statusCounts[tab.key] ?? 0 }}）</option>
  </select>
 </section>

 <!-- 文章列表 -->
 <section class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <!-- 骨架屏 - loading 期间显示，形态与真实卡片一致 -->
 <div v-if="loading" class="flex flex-col gap-[var(--spacer-12)]">
 <div
 v-for="i in skeletonRows"
 :key="i"
 class="rounded-[var(--radius-12)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] p-[var(--spacer-16)]"
 >
 <div class="flex gap-[var(--spacer-12)]">
 <div class="h-12 w-12 rounded-full bg-[var(--bg-overlay-l2)] skeleton-pulse shrink-0" />
 <div class="flex-1 space-y-[var(--spacer-6)]">
 <div class="h-4 w-4/5 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l2)] skeleton-pulse" />
 <div class="h-4 w-3/5 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l2)] skeleton-pulse" />
 <div class="h-3 w-1/2 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
 </div>
 <div class="h-5 w-14 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse shrink-0" />
 </div>
 <div class="mt-[var(--spacer-12)] border-t border-[var(--border-neutral-l1)] pt-[var(--spacer-12)]">
 <div class="h-4 w-24 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
 </div>
 </div>
 </div>

 <!-- 块状卡片列表：标题两行截断 + 摘要 + meta 独占整行 + 操作区下沉到 footer，告别单行挤压 -->
 <div v-else-if="filteredArticles.length > 0" class="flex flex-col gap-[var(--spacer-12)]">
 <article
 v-for="article in filteredArticles"
 :key="article.id"
 class="ds-list-item ds-list-item--block"
 @click="goEdit(article.id)"
 >
 <div class="ds-list-item__head">
 <div class="flex min-w-0 flex-1 items-center gap-[var(--spacer-12)]">
 <span
 class="ds-list-item__icon"
 :class="statusIconVariant[article.status]"
 >
 <FileText :size="20" />
 </span>
 <span class="min-w-0 font-heading text-heading-sm font-semibold text-text line-clamp-2">
 {{ article.title }}
 </span>
 </div>
 <span class="ds-tag ds-tag--plain shrink-0" :class="'ds-tag--' + statusTagType[article.status]">{{ statusConfig[article.status].label }}</span>
 </div>

 <p v-if="article.summary" class="ds-list-item__summary">{{ stripHtml(article.summary) }}</p>

 <div class="ds-list-item__meta mt-[var(--spacer-8)]">
 <span class="truncate">{{ article.department_name }}</span>
 <span class="text-[var(--border-neutral-l2)]" aria-hidden="true">·</span>
 <span class="shrink-0">{{ metaText(article) }}</span>
 <template v-if="article.status === 'published' && article.view_count > 0">
 <span class="text-[var(--border-neutral-l2)]" aria-hidden="true">·</span>
 <span class="inline-flex items-center gap-[var(--spacer-2)] shrink-0">
 <Eye :size="12" />
 {{ fmtCompact(article.view_count) }}
 </span>
 </template>
 </div>

 <div class="ds-list-item__footer">
 <div class="ds-list-item__actions" @click.stop>
 <button
 v-if="article.status === 'published'"
 type="button"
 class="ds-list-item__action-btn"
 aria-label="归档"
 @click="handleArchive(article.id)"
 >
 <Archive :size="16" />
 </button>
 <button
 v-if="article.status === 'archived'"
 type="button"
 class="ds-list-item__action-btn text-[var(--status-success-default)]"
 aria-label="取消归档"
 @click="handleUnarchive(article.id)"
 >
 <ArchiveRestore :size="16" />
 </button>
 <template v-if="canManageFeatured && article.status === 'published'">
 <button
 v-for="rank in [1, 2, 3]"
 :key="rank"
 type="button"
 class="ds-list-item__action-btn"
 :class="article.featured_rank === rank ? 'text-text-brand' : ''"
 :aria-label="`设为热门 ${rank}`"
 @click="handleFeatured(article, article.featured_rank === rank ? 0 : rank)"
 >
 <Star :size="16" :fill="article.featured_rank === rank ? 'currentColor' : 'none'" />
 </button>
 </template>
 <button
 v-if="article.status !== 'deleted'"
 type="button"
 class="ds-list-item__action-btn"
 aria-label="删除"
 @click="handleDelete(article.id)"
 >
 <Trash2 :size="16" />
 </button>
 </div>
 <span class="inline-flex items-center gap-[var(--spacer-2)] text-body-sm text-text-tertiary">
 编辑
 <ChevronRight :size="16" class="text-icon-tertiary" />
 </span>
 </div>
 </article>
 </div>

 <!-- 增强空状态：图标 + 引导文案 + CTA -->
 <div
 v-if="!loading && filteredArticles.length === 0"
 class="flex flex-col items-center justify-center px-[var(--spacer-32)] py-[var(--spacer-48)] text-center"
 >
 <span class="flex items-center justify-center w-16 h-16 rounded-[var(--radius-full)] bg-[var(--bg-brand-light)] text-text-brand mb-[var(--spacer-16)]">
 <PenLine :size="28" />
 </span>
 <p class="font-heading text-heading-sm font-semibold text-text mb-[var(--spacer-4)]">
 {{ searchQuery || activeTab !== 'all' ? '没有匹配的文章' : '还没有文章' }}
 </p>
 <p class="text-body-md text-text-tertiary mb-[var(--spacer-24)]">
 {{ searchQuery || activeTab !== 'all' ? '试试调整筛选条件或搜索关键词' : '开始创作你的第一篇科普文章吧' }}
 </p>
 <button
 v-if="!searchQuery && activeTab === 'all'"
 type="button"
 class="ds-btn ds-btn--primary ds-btn--pill"
 @click="goCreate"
 >
 <PenLine :size="16" />
 写一篇
 </button>
 </div>
 </section>

 </main>
</template>


