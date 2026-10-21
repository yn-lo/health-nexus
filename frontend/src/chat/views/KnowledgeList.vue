<script setup lang="ts">
/**
 * KnowledgeList 健康知识库 - v3 风格（ui-ux-pro-max: Content-First + Accessible & Ethical）
 * 风格升级要点：
 * - 触摸目标全部 ≥44pt（搜索/分类按钮）- WCAG AAA
 * - 置顶与全部文章合并为单一列表，置顶仅以图标底色（alert vs brand）区分
 * - 列表卡片化容器 + hover 阴影微反馈
 * - meta 用图标点分隔，提升可读性
 * - 加载骨架屏（pulse 动画，避免空白闪烁）
 * - 搜索栏常驻 + 服务端搜索（title/summary ILIKE + debounce 300ms）
 * - 科室筛选使用 DepartmentTabs 共享组件（后端按 department_id 筛选 + 真分页）
 * - 列表项可展开摘要（右侧 chevron，点击 @click.stop 仅展开/收起，不跳转）
 * 保留功能: wikiApi.listArticles, 分类筛选, 搜索, goArticle 跳转
 */
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { Search, BookOpen } from '@lucide/vue'
import { wikiApi } from '@/shared'
import { useDepartments } from '@/chat/composables/useDepartments'
import { EmptyState, DepartmentTabs, DsPullRefresh } from '@/shared/components'
import { useDsToast } from '@/shared/composables'
import KnowledgeListItem from '@/chat/components/KnowledgeListItem.vue'
import type { ArticlePublic } from '@/shared'

withDefaults(defineProps<{
  /** 嵌入模式：隐藏自带 header，由父组件提供导航 */
  embedded?: boolean
}>(), {
  embedded: false,
})

const router = useRouter()
const { showFailToast } = useDsToast()

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

/** 置顶文章（仅 featured_rank > 0 才是医护端设置的置顶；搜索时不提升置顶） */
const pinnedArticles = computed(() => {
  if (searchQuery.value.trim()) return []
  return featuredArticles.value.filter((a) => a.featured_rank > 0)
})

const pinnedIds = computed(() => new Set(pinnedArticles.value.map((a) => a.id)))

/** 单一列表：置顶在前 + 全部文章（按 id 去重，避免置顶文章在列表里重复出现） */
const mergedArticles = computed(() => {
  const seen = new Set<number>()
  const merged: ArticlePublic[] = []
  for (const article of [...pinnedArticles.value, ...articles.value]) {
    if (seen.has(article.id)) continue
    seen.add(article.id)
    merged.push(article)
  }
  return merged
})

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

/** 触底加载：哨兵进入视口（提前 120px）时拉下一页 */
const loadSentinel = ref<HTMLDivElement | null>(null)
const containerRef = ref<HTMLElement | null>(null)
let loadObserver: IntersectionObserver | null = null

function observeSentinel() {
  loadObserver?.disconnect()
  if (loadSentinel.value) {
    loadObserver = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) void onLoad()
      },
      { root: containerRef.value, rootMargin: '120px' },
    )
    loadObserver.observe(loadSentinel.value)
  }
}

/** 加载下一页拼接 */
async function onLoad() {
  if (listLoading.value || listFinished.value || refreshing.value) return
  listLoading.value = true
  try {
    await fetchPage(page.value + 1, false)
  } catch {
    showFailToast('加载失败')
  } finally {
    listLoading.value = false
  }
}

// 首次/科室切换/搜索加载完成后再挂载哨兵观察；重新加载时先断开
watch(loading, (val) => {
  if (val) {
    loadObserver?.disconnect()
  } else {
    nextTick(observeSentinel)
  }
})

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
  loadObserver?.disconnect()
})
</script>

<template>
  <div ref="containerRef" class="knowledge-list flex flex-col bg-[var(--bg-base-secondary)] overflow-y-auto no-scrollbar" :class="embedded ? 'flex-1 min-h-0' : 'min-h-[calc(100dvh-var(--layout-tabbar-height))]'">
    <!-- 顶部栏（白色背景，与下方内容区分）- 嵌入模式由父组件提供 -->
    <header v-if="!embedded" class="sticky top-0 z-30 flex items-center justify-between px-[var(--spacer-16)] py-[var(--spacer-12)] border-b border-[var(--border-neutral-l1)] bg-[var(--bg-base-default)]">
      <h1 class="truncate font-heading text-heading-md font-semibold text-text">
        健康知识库
      </h1>
    </header>

    <!-- 科室筛选（下拉）+ 搜索栏 - 同一行，科室在左，搜索框占满剩余 -->
    <div class="flex items-center gap-[var(--spacer-8)] px-[var(--spacer-16)] py-[var(--spacer-8)] bg-[var(--bg-base-default)] border-b border-[var(--border-neutral-l1)]">
      <DepartmentTabs
        v-model="activeDepartmentId"
        :options="tabOptions"
        @update:model-value="onDepartmentChange"
      />
      <div class="ds-search-box ds-search-box--md flex-1 min-w-0">
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

      <!-- 分隔标题 -->
      <div v-if="!loading && mergedArticles.length" class="flex items-center justify-between mb-[var(--spacer-12)]">
        <h2 class="truncate font-heading text-heading-sm font-semibold text-text">
          {{ searchQuery.trim() ? '搜索结果' : '全部文章' }}
        </h2>
        <span class="shrink-0 text-body-sm text-text-tertiary">
          共 {{ total }} 篇
        </span>
      </div>

      <!-- 文章列表 - 置顶文章排在最前，仅用图标底色区分，不再单独分区 -->
      <DsPullRefresh v-if="!loading" :loading="refreshing" @refresh="onRefresh">
        <div class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden shadow-[var(--shadow-xs)]">
          <div
            v-for="article in mergedArticles"
            :key="article.id"
            class="ds-list-item--divider"
          >
            <KnowledgeListItem
              :article="article"
              :icon="BookOpen"
              :tone="pinnedIds.has(article.id) ? 'alert' : 'brand'"
              :expanded="expandedIds.has(article.id)"
              @open="goArticle"
              @toggle="toggleExpand"
            />
          </div>
        </div>

        <!-- 触底哨兵 + 加载状态 -->
        <div ref="loadSentinel" class="py-[var(--spacer-8)] min-h-[2px]">
          <div v-if="listLoading" class="ds-loading py-[var(--spacer-8)]">
            <span class="ds-loading__spinner ds-loading__spinner--sm" />
          </div>
          <p v-else-if="listFinished && mergedArticles.length" class="text-center text-body-xs text-text-tertiary m-0 py-[var(--spacer-8)]">
            没有更多了
          </p>
        </div>
      </DsPullRefresh>

      <!-- 空状态 - v3 风格：图标 + 文案 -->
      <EmptyState v-if="!loading && mergedArticles.length === 0" :text="searchQuery.trim() ? '未找到相关文章' : '暂无文章'" />
    </section>
  </div>
</template>
