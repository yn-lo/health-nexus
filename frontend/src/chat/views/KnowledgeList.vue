<script setup lang="ts">
/**
 * KnowledgeList 健康知识库 - v3 风格（ui-ux-pro-max: Content-First + Accessible & Ethical）
 * 风格升级要点：
 * - 触摸目标全部 ≥44pt（搜索/分类按钮）- WCAG AAA
 * - 精选卡头像渐变光晕（hero-orb 视觉锤）
 * - 列表卡片化容器 + hover 阴影微反馈
 * - meta 用图标点分隔，提升可读性
 * - 加载骨架屏（pulse 动画，避免空白闪烁）
 * - 搜索栏常驻 + 服务端搜索（title/summary ILIKE + debounce 300ms）
 * - 科室筛选使用 DepartmentTabs 共享组件（后端按 department_id 筛选 + 真分页）
 * - 列表项可展开摘要（右侧 chevron，点击 @click.stop 仅展开/收起，不跳转）
 * 保留功能: wikiApi.listArticles, 分类筛选, 搜索, goArticle 跳转
 */
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Search, BookOpen, Clock, Eye, ChevronDown, Calendar } from '@lucide/vue'
import { List as VanList, PullRefresh as VanPullRefresh, Swipe as VanSwipe, SwipeItem as VanSwipeItem, showFailToast } from 'vant'
import MarkdownIt from 'markdown-it'
import { wikiApi, fmtCompact, fmtShortDate } from '@/shared'
import { useDepartments } from '@/chat/composables/useDepartments'
import { EmptyState, DepartmentTabs } from '@/shared/components'
import type { ArticlePublic } from '@/shared'

withDefaults(defineProps<{
  /** 嵌入模式：隐藏自带 header，由父组件提供导航 */
  embedded?: boolean
}>(), {
  embedded: false,
})

const router = useRouter()

const { departments, fetchDepartments } = useDepartments({ autoFetch: false, filter: 'active' })

const PAGE_SIZE = 20
const SEARCH_DEBOUNCE_MS = 300

const articles = ref<ArticlePublic[]>([])
const featuredArticles = ref<ArticlePublic[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const refreshing = ref(false)
const listLoading = ref(false)
const listFinished = ref(false)
const activeDepartmentId = ref(0) // 0 = 全部
const searchQuery = ref('')
/** 展开摘要的文章 ID 集合 */
const expandedIds = ref<Set<number>>(new Set())

/** DepartmentTabs 选项：直接复用 useDepartments 的列表（首项为 ALL_DEPARTMENTS, id=0） */
const tabOptions = computed(() =>
  departments.value.map((d) => ({ id: d.id, label: d.name })),
)

/** 精选文章（搜索时不显示轮播，避免与列表结果重复且聚焦搜索结果） */
const visibleFeaturedArticles = computed(() => {
  if (searchQuery.value.trim()) return []
  return featuredArticles.value
})

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

/** 跳转文章详情 */
function goArticle(id: number) {
  router.push({ name: 'wiki-article', params: { id } })
}

/** 切换某篇文章的摘要展开状态（不影响整行跳转） */
function toggleExpand(id: number) {
  const next = new Set(expandedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expandedIds.value = next
}

/** 拉取指定页（按当前科室 + 搜索词），拼接或替换 articles */
async function fetchFeatured() {
  const res = await wikiApi.listFeaturedArticles(activeDepartmentId.value || undefined)
  featuredArticles.value = res.items
}

async function fetchPage(targetPage: number, replace: boolean) {
  const params: { page: number; page_size: number; department_id?: number; search?: string } = {
    page: targetPage,
    page_size: PAGE_SIZE,
  }
  if (activeDepartmentId.value !== 0) params.department_id = activeDepartmentId.value
  const q = searchQuery.value.trim()
  if (q) params.search = q
  const res = await wikiApi.listArticles(params)
  if (replace) {
    articles.value = res.items
  } else {
    articles.value.push(...res.items)
  }
  total.value = res.total
  page.value = targetPage
  listFinished.value = articles.value.length >= res.total
}

/** 下拉刷新：重置到第 1 页 */
async function onRefresh() {
  refreshing.value = true
  try {
    await Promise.all([fetchPage(1, true), fetchFeatured()])
  } catch {
    showFailToast('刷新失败')
  } finally {
    refreshing.value = false
  }
}

/** van-list 加载：拉下一页拼接 */
async function onLoad() {
  if (listFinished.value) return
  try {
    await fetchPage(page.value + 1, false)
  } catch {
    showFailToast('加载失败')
  } finally {
    listLoading.value = false
  }
}

/** 切换科室：重置分页并拉取第 1 页 */
async function onDepartmentChange() {
  loading.value = true
  try {
    await Promise.all([fetchPage(1, true), fetchFeatured()])
  } catch {
    showFailToast('加载失败')
  } finally {
    loading.value = false
  }
}

/** 搜索词变更（debounce）：重置分页并拉取第 1 页 */
let searchTimer: ReturnType<typeof setTimeout> | null = null
watch(searchQuery, () => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(async () => {
    expandedIds.value = new Set()
    loading.value = true
    try {
      await fetchPage(1, true)
    } catch {
      showFailToast('搜索失败')
    } finally {
      loading.value = false
    }
  }, SEARCH_DEBOUNCE_MS)
})

/** 骨架占位条数 */
const skeletonRows = [0, 1, 2, 3]

onMounted(async () => {
  loading.value = true
  try {
    // 并行拉取文章和科室列表；科室失败时仅退化分类筛选，不阻塞文章展示
    await Promise.all([
      fetchPage(1, true),
      fetchFeatured(),
      fetchDepartments().catch(() => {}),
    ])
  } catch {
    showFailToast('加载失败')
  } finally {
    loading.value = false
  }
})

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer)
})
</script>

<template>
  <div class="knowledge-list flex flex-col bg-[var(--bg-base-secondary)] overflow-y-auto no-scrollbar" :class="embedded ? 'flex-1 min-h-0' : 'min-h-[calc(100dvh-var(--layout-tabbar-height))]'">
    <!-- 顶部栏（白色背景，与下方内容区分）- 嵌入模式由父组件提供 -->
    <header v-if="!embedded" class="sticky top-0 z-30 flex items-center justify-between px-[var(--spacer-16)] py-[var(--spacer-12)] border-b border-[var(--border-neutral-l1)] bg-[var(--bg-base-default)]">
      <h1 class="truncate font-heading text-heading-md font-semibold text-text">
        健康知识库
      </h1>
    </header>

    <!-- 科室筛选 - 共享组件 DepartmentTabs -->
    <DepartmentTabs
      v-model="activeDepartmentId"
      :options="tabOptions"
      :sticky-top="embedded ? '' : '56px'"
      @update:model-value="onDepartmentChange"
    />

    <!-- 搜索栏 - ds-search-box 品牌光晕（常驻，贴近结果区） -->
    <div class="px-[var(--spacer-16)] py-[var(--spacer-8)] bg-[var(--bg-base-default)] border-b border-[var(--border-neutral-l1)]">
      <div class="ds-search-box ds-search-box--md">
        <Search class="ds-search-box__icon h-4 w-4" />
        <input
          v-model="searchQuery"
          type="text"
          inputmode="search"
          placeholder="搜索健康文章标题或摘要"
          class="ds-search-box__input"
        >
      </div>
    </div>

    <!-- 内容区 -->
    <section class="flex-1 px-[var(--spacer-16)] py-[var(--spacer-16)]">
      <!-- 骨架屏 - loading 期间显示，避免空白闪烁 -->
      <div v-if="loading" class="space-y-[var(--spacer-16)]">
        <!-- 精选骨架 -->
        <div class="rounded-[var(--radius-card-soft)] overflow-hidden bg-[var(--bg-base-default)] shadow-[var(--shadow-md)]">
          <div class="p-[var(--spacer-20)] space-y-[var(--spacer-12)]">
            <div class="h-7 w-20 rounded-[var(--radius-full)] bg-[var(--bg-overlay-l2)] skeleton-pulse" />
            <div class="h-6 w-3/4 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l2)] skeleton-pulse" />
            <div class="h-4 w-full rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
            <div class="h-4 w-2/3 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
          </div>
        </div>
        <!-- 列表骨架 -->
        <div class="rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
          <div
            v-for="i in skeletonRows"
            :key="i"
            class="flex items-center gap-[var(--spacer-12)] px-[var(--spacer-16)] py-[var(--spacer-12)] border-b border-[var(--border-neutral-l1)]"
          >
            <div class="w-12 h-12 rounded-full bg-[var(--bg-overlay-l2)] skeleton-pulse shrink-0" />
            <div class="flex-1 space-y-[var(--spacer-6)]">
              <div class="h-4 w-3/4 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l2)] skeleton-pulse" />
              <div class="h-3 w-1/2 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
            </div>
          </div>
        </div>
      </div>

      <!-- 精选文章 - 按浏览量 Top3 轮播（视觉锤头像 + hover 上浮） -->
      <section v-if="visibleFeaturedArticles.length" class="mb-[var(--spacer-24)]">
        <VanSwipe
          :autoplay="4000"
          :show-indicators="visibleFeaturedArticles.length > 1"
          indicator-color="var(--bg-brand)"
        >
          <!-- SwipeItem 内边距：为卡片阴影与 hover 上浮预留空间，避免被 overflow:hidden 裁剪 -->
          <VanSwipeItem
            v-for="article in visibleFeaturedArticles"
            :key="article.id"
            class="px-[var(--spacer-4)] pt-[var(--spacer-4)] pb-[var(--spacer-12)]"
          >
            <a
              class="block rounded-[var(--radius-card-soft)] overflow-hidden bg-[var(--bg-base-default)] shadow-[var(--shadow-md)] hover:shadow-[var(--shadow-lg)] hover:-translate-y-0.5 transition-[box-shadow,transform_var(--micro-duration)_var(--micro-ease)] cursor-pointer"
              @click="goArticle(article.id)"
            >
              <div class="p-[var(--spacer-20)]">
                <!-- 科室标签 - 品牌配色 -->
                <span class="inline-flex items-center h-7 px-[var(--spacer-12)] rounded-[var(--radius-full)] mb-[var(--spacer-12)] bg-[var(--bg-brand-light)] text-text-brand text-body-sm font-medium ">
                  {{ article.department_name }}
                </span>
                <h2 class="line-clamp-2 mb-[var(--spacer-10)] font-heading text-heading-md leading-[1.3] font-semibold text-text [text-wrap:balance]">
                  {{ article.title }}
                </h2>
                <p class="line-clamp-2 mb-[var(--spacer-16)] text-body-base leading-[1.6] text-text-secondary">
                  {{ renderPlainText(article.summary) }}
                </p>
                <div class="flex items-center justify-between">
                  <div class="flex items-center gap-[var(--spacer-10)] min-w-0">
                    <span class="inline-flex items-center justify-center shrink-0 w-8 h-8 rounded-full bg-gradient-to-br from-[var(--bg-brand)] to-[var(--accent-violet)] text-onbrand text-body-sm font-semibold shadow-[var(--shadow-sm)]">
                      {{ (article.author_name || '医').charAt(0) }}
                    </span>
                    <span class="truncate text-body-sm text-text-secondary">
                      {{ article.author_name }}
                    </span>
                  </div>
                  <div class="flex items-center gap-[var(--spacer-12)] shrink-0 ml-[var(--spacer-12)] text-text-tertiary">
                    <span class="inline-flex items-center gap-[var(--spacer-4)] text-body-sm">
                      <Clock :size="12" />
                      {{ estimateReadTime(article.summary) }} 分钟
                    </span>
                    <span class="inline-flex items-center gap-[var(--spacer-4)] text-body-sm">
                      <Eye :size="12" />
                      {{ fmtCompact(article.view_count) }}
                    </span>
                  </div>
                </div>
              </div>
            </a>
          </VanSwipeItem>
        </VanSwipe>
      </section>

      <!-- 分隔标题 -->
      <div v-if="!loading && articles.length" class="flex items-center justify-between mb-[var(--spacer-12)]">
        <h2 class="truncate font-heading text-heading-sm font-semibold text-text">
          {{ searchQuery.trim() ? '搜索结果' : '全部文章' }}
        </h2>
        <span class="shrink-0 text-body-sm text-text-tertiary">
          共 {{ total }} 篇
        </span>
      </div>

      <!-- 文章列表 - v3 风格：卡片化容器 + 图标化 meta + chevron 展开摘要 -->
      <VanPullRefresh v-if="!loading" v-model="refreshing" @refresh="onRefresh">
        <VanList
          v-model:loading="listLoading"
          :finished="listFinished"
          finished-text=""
          @load="onLoad"
        >
          <div class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden shadow-[var(--shadow-xs)]">
            <div
              v-for="article in articles"
              :key="article.id"
              class="ds-list-item--divider"
            >
              <!-- 主行：图标 + 内容 + chevron（flex 横排） -->
              <div class="ds-list-item min-h-[var(--touch-target-min)]">
                <span class="ds-list-item__icon ds-list-item__icon--brand">
                  <BookOpen :size="20" />
                </span>
                <div class="ds-list-item__content" @click="goArticle(article.id)">
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
                  :aria-label="expandedIds.has(article.id) ? '收起摘要' : '展开摘要'"
                  :aria-expanded="expandedIds.has(article.id)"
                  @click.stop="toggleExpand(article.id)"
                >
                  <ChevronDown
                    :size="18"
                    class="transition-transform duration-200"
                    :class="expandedIds.has(article.id) ? 'rotate-180' : ''"
                  />
                </button>
              </div>
              <!-- 展开后的摘要（整行宽度，主行下方） -->
              <div
                v-if="expandedIds.has(article.id)"
                class="px-[var(--spacer-16)] pb-[var(--spacer-12)] text-body-sm leading-[1.6] text-text-secondary"
              >
                {{ renderPlainText(article.summary) }}
              </div>
            </div>
          </div>
        </VanList>
      </VanPullRefresh>

      <!-- 空状态 - v3 风格：图标 + 文案 -->
      <EmptyState v-if="!loading && articles.length === 0" :text="searchQuery.trim() ? '未找到相关文章' : '暂无文章'" />
    </section>
  </div>
</template>
