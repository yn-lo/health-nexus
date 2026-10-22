<script setup lang="ts">
/**
 * 医护端文章管理列表
 * 信息架构：搜索/科室筛选 → 状态 chips → 置顶组（可排序）→ 文章列表（滚动加载）
 * - 服务端列表同样置顶优先，因此 ↑↓ 调整后卡片位置会真实变化，操作反馈可见
 * - 行内操作统一收进 ⋮ 动作面板，破坏性操作（删除）与常用操作不再同权重
 * - 状态只编码两次：圆点表示状态，非已发布才补一个文字标签
 * API: wikiApi.listMyArticles() + 置顶/归档/删除接口
 */
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import {
  Plus, Trash2, Search, FileText, ChevronUp, ChevronDown,
  Archive, ArchiveRestore, PenLine, Pin, EllipsisVertical,
} from '@lucide/vue'
import { useDsToast, useDsDialog, useDepartmentOptions } from '@/shared/composables'
import { AppHeader, DepartmentTabs, DsPopup } from '@/shared/components'
import { wikiApi, fmtShortDate, fmtCompact } from '@/shared'
import { errmsg } from '@/shared/api/client'
import type { ArticleStaff, ArticleStatus } from '@/shared'
import { useAuthStore } from '@/stores/auth'
import { ADMIN_ROLES, SUPER_ADMIN_ROLE } from '@/shared/constants/roles'

const router = useRouter()
const authStore = useAuthStore()
const { showSuccessToast, showFailToast } = useDsToast()
const { showConfirmDialog } = useDsDialog()

/** 每页条数（滚动到底自动加载下一页） */
const PAGE_SIZE = 20

const articles = ref<ArticleStaff[]>([])
const total = ref(0)
const currentPage = ref(0)
const loading = ref(false)
const loadingMore = ref(false)
const searchQuery = ref('')
const activeTab = ref('all')

const canManageFeatured = computed(() => Boolean(authStore.user && ADMIN_ROLES.includes(authStore.user.role)))
const isSuperAdmin = computed(() => authStore.user?.role === SUPER_ADMIN_ROLE)

/** 科室筛选仅超管可见；非超管由服务端按本科室隔离 */
const { options: deptOptions, load: loadDeptOptions } = useDepartmentOptions({ id: null, label: '全部科室' })
const deptTabOptions = computed(() =>
  deptOptions.value.map((o) => ({ id: o.id ?? 'all', label: o.label })),
)
const deptFilter = ref<number | string>('all')

const scopedDeptId = computed(() =>
  isSuperAdmin.value && typeof deptFilter.value === 'number' ? deptFilter.value : undefined,
)

/** 状态筛选（服务端仅提供单状态，chips 在前端本地过滤已加载数据） */
const tabItems = [
  { key: 'all', label: '全部' },
  { key: 'published', label: '已发布' },
  { key: 'pending', label: '待审核' },
  { key: 'draft', label: '草稿' },
  { key: 'archived', label: '已归档' },
]

const statusConfig: Record<ArticleStatus, { label: string }> = {
  published: { label: '已发布' },
  pending: { label: '待审核' },
  draft: { label: '草稿' },
  archived: { label: '已归档' },
  deleted: { label: '已删除' },
}

/** 非已发布状态才显示文字标签，已发布仅靠圆点区分，减少重复编码 */
const statusTagType: Record<ArticleStatus, 'success' | 'primary' | 'default'> = {
  published: 'success',
  pending: 'primary',
  draft: 'default',
  archived: 'default',
  deleted: 'default',
}

const statusDotClass: Record<ArticleStatus, string> = {
  published: 'bg-[var(--status-success-default)]',
  pending: 'bg-[var(--bg-brand)]',
  draft: 'bg-[var(--text-tertiary)]',
  archived: 'bg-[var(--border-neutral-l2)]',
  deleted: 'bg-[var(--border-neutral-l2)]',
}

/** 已发布显示发布日期，其余显示最近更新时间 */
function metaText(article: ArticleStaff): string {
  if (article.status === 'published' && article.published_at) {
    return `发布于 ${fmtShortDate(article.published_at)}`
  }
  return `更新于 ${fmtShortDate(article.updated_at)}`
}

/** 是否存在未加载的下一页 */
const hasMore = computed(() => articles.value.length < total.value)

async function fetchPage(target: number, replace: boolean) {
  const res = await wikiApi.listMyArticles({
    page: target,
    page_size: PAGE_SIZE,
    department_id: scopedDeptId.value,
  })
  total.value = res.total
  currentPage.value = target
  articles.value = replace ? res.items : [...articles.value, ...res.items]
}

/** 首屏/筛选变化时重新加载 */
async function reload() {
  loading.value = true
  try {
    await fetchPage(1, true)
  } catch (e) {
    showFailToast(errmsg(e, '加载失败'))
  } finally {
    loading.value = false
  }
}

async function loadMore() {
  if (loading.value || loadingMore.value || !hasMore.value) return
  loadingMore.value = true
  try {
    await fetchPage(currentPage.value + 1, false)
  } catch (e) {
    showFailToast(errmsg(e, '加载更多失败'))
  } finally {
    loadingMore.value = false
  }
}

/** 触底哨兵：进入视口即加载下一页 */
const sentinel = ref<HTMLElement | null>(null)
let observer: IntersectionObserver | null = null
watch(sentinel, (el) => {
  observer?.disconnect()
  if (!el) return
  observer = new IntersectionObserver(
    (entries) => {
      if (entries[0]?.isIntersecting) loadMore()
    },
    { rootMargin: '200px' },
  )
  observer.observe(el)
})
onUnmounted(() => observer?.disconnect())

// ===== 置顶 =====

/** 置顶组只在「全部 + 无搜索 + 已限定科室」时展示：跨科室置顶顺序没有可比性 */
const showFeaturedGroup = computed(
  () =>
    activeTab.value === 'all' &&
    !searchQuery.value.trim() &&
    (!isSuperAdmin.value || typeof deptFilter.value === 'number'),
)

/** 置顶文章按科室内顺序排列（超管跨科室时先按科室分组） */
const featuredGroup = computed(() => {
  if (!showFeaturedGroup.value) return []
  return articles.value
    .filter((a) => a.featured_rank > 0)
    .sort(
      (a, b) =>
        (a.department_id ?? 0) - (b.department_id ?? 0) || a.featured_rank - b.featured_rank,
    )
})

/** 普通列表：置顶组展示时排除置顶文章，避免同一篇出现两次 */
const plainArticles = computed(() => {
  let list = activeTab.value === 'all'
    ? articles.value
    : articles.value.filter((a) => a.status === activeTab.value)
  if (showFeaturedGroup.value) list = list.filter((a) => a.featured_rank === 0)
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return list
  return list.filter(
    (a) =>
      a.title.toLowerCase().includes(q) ||
      a.author_name.toLowerCase().includes(q) ||
      a.department_name.toLowerCase().includes(q),
  )
})

/** 本科室下一个置顶位（末位 + 1，置顶不限数量） */
function nextFeaturedRank(article: ArticleStaff): number {
  const ranks = articles.value
    .filter((a) => a.department_id === article.department_id && a.featured_rank > 0)
    .map((a) => a.featured_rank)
  return ranks.length ? Math.max(...ranks) + 1 : 1
}

/** 同科室置顶文章，按顺序排列（用于上下移的边界判断与交换对象定位） */
function featuredSiblings(article: ArticleStaff): ArticleStaff[] {
  return articles.value
    .filter((a) => a.department_id === article.department_id && a.featured_rank > 0)
    .sort((a, b) => a.featured_rank - b.featured_rank)
}

function canMove(article: ArticleStaff, direction: 'up' | 'down'): boolean {
  const siblings = featuredSiblings(article)
  const index = siblings.findIndex((a) => a.id === article.id)
  return direction === 'up' ? index > 0 : index >= 0 && index < siblings.length - 1
}

async function handleFeatured(article: ArticleStaff, rank: number) {
  try {
    await wikiApi.setArticleFeatured(article.id, rank)
    article.featured_rank = rank
    showSuccessToast(rank ? '已置顶' : '已取消置顶')
  } catch (e) {
    showFailToast(errmsg(e, rank ? '设置置顶失败' : '取消置顶失败'))
  }
}

/** 置顶开关：已置顶→取消；未置顶→排到本科室置顶序列末尾 */
async function toggleFeatured(article: ArticleStaff) {
  await handleFeatured(article, article.featured_rank > 0 ? 0 : nextFeaturedRank(article))
}

/** 上移/下移：服务端在事务内交换顺序，成功后本地同步交换，避免整表重载打断滚动位置 */
const moving = ref(false)
async function moveFeatured(article: ArticleStaff, direction: 'up' | 'down') {
  if (moving.value) return
  const siblings = featuredSiblings(article)
  const index = siblings.findIndex((a) => a.id === article.id)
  const neighbor = direction === 'up' ? siblings[index - 1] : siblings[index + 1]
  if (!neighbor) return
  moving.value = true
  try {
    await wikiApi.moveArticleFeatured(article.id, direction)
    const rank = article.featured_rank
    article.featured_rank = neighbor.featured_rank
    neighbor.featured_rank = rank
  } catch (e) {
    showFailToast(errmsg(e, '调整置顶顺序失败'))
  } finally {
    moving.value = false
  }
}

// ===== 行内动作面板 =====

const actionArticle = ref<ArticleStaff | null>(null)

function openActions(article: ArticleStaff) {
  actionArticle.value = article
}

function closeActions() {
  actionArticle.value = null
}

function runSheetAction(action: (article: ArticleStaff) => void | Promise<void>) {
  const target = actionArticle.value
  closeActions()
  if (target) void action(target)
}

/** 新建文章 */
function goCreate() {
  router.push({ name: 'staff-article-create' })
}

/** 编辑文章 */
function goEdit(id: number) {
  router.push({ name: 'staff-article-edit', params: { id } })
}

/** 删除文章（带确认弹窗） */
async function handleDelete(article: ArticleStaff) {
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
    await wikiApi.deleteArticle(article.id)
    articles.value = articles.value.filter((a) => a.id !== article.id)
    total.value = Math.max(0, total.value - 1)
    showSuccessToast('已删除')
  } catch (e) {
    showFailToast(errmsg(e, '删除失败'))
  }
}

/** 归档文章（published → archived） */
async function handleArchive(article: ArticleStaff) {
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
    await wikiApi.archiveArticle(article.id)
    article.status = 'archived'
    showSuccessToast('已归档')
  } catch (e) {
    showFailToast(errmsg(e, '归档失败'))
  }
}

/** 取消归档（archived → published） */
async function handleUnarchive(article: ArticleStaff) {
  try {
    await wikiApi.unarchiveArticle(article.id)
    article.status = 'published'
    showSuccessToast('已取消归档')
  } catch (e) {
    showFailToast(errmsg(e, '取消归档失败'))
  }
}

// 超管可切换科室，切科室需重新从第一页拉取
watch(deptFilter, () => {
  void reload()
})

onMounted(async () => {
  if (isSuperAdmin.value) void loadDeptOptions()
  await reload()
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

    <!-- 搜索 + 科室筛选（超管）：一行内完成，不再叠加统计卡 -->
    <section class="px-[var(--spacer-16)] pt-[var(--spacer-12)] pb-[var(--spacer-8)]">
      <div class="flex items-center gap-[var(--spacer-8)]">
        <div class="ds-search-box ds-search-box--md min-w-0 flex-1">
          <Search class="h-4 w-4 shrink-0 text-icon-brand" />
          <input
            v-model="searchQuery"
            type="search"
            inputmode="search"
            placeholder="搜索文章标题..."
            class="min-w-0 flex-1 border-none bg-transparent font-heading text-body-base text-text outline-none placeholder:text-text-tertiary"
          >
        </div>
        <DepartmentTabs v-if="isSuperAdmin" v-model="deptFilter" :options="deptTabOptions" />
      </div>
    </section>

    <!-- 状态筛选：胶囊横滑，替代原生 select（计数随分页变化，不再展示避免误导） -->
    <section class="px-[var(--spacer-16)] pb-[var(--spacer-12)]">
      <div class="flex items-center gap-[var(--spacer-8)] overflow-x-auto no-scrollbar">
        <button
          v-for="tab in tabItems"
          :key="tab.key"
          type="button"
          class="ds-pill ds-pill--sm shrink-0"
          :class="activeTab === tab.key ? 'ds-pill--selected' : 'ds-pill--unselected'"
          :aria-pressed="activeTab === tab.key"
          @click="activeTab = tab.key"
        >
          {{ tab.label }}
        </button>
      </div>
    </section>

    <section class="px-[var(--spacer-16)]">
      <!-- 骨架屏：与紧凑行同形态，避免加载时布局跳动 -->
      <div v-if="loading" class="flex flex-col gap-[var(--spacer-8)]">
        <div
          v-for="i in 5"
          :key="i"
          class="rounded-[var(--radius-12)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]"
        >
          <div class="h-4 w-4/5 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l2)] skeleton-pulse" />
          <div class="mt-[var(--spacer-6)] h-3 w-1/2 rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] skeleton-pulse" />
        </div>
      </div>

      <template v-else>
        <!-- 置顶组：序号 + 患者端顺序，↑↓ 调整后位置即时变化 -->
        <div v-if="featuredGroup.length" class="mb-[var(--spacer-16)]">
          <p class="m-0 mb-[var(--spacer-8)] text-body-xs font-medium text-text-tertiary">
            置顶 · {{ featuredGroup.length }}
            <span class="font-normal">（患者端按此顺序展示）</span>
          </p>
          <ul class="m-0 p-0 list-none flex flex-col gap-[var(--spacer-8)]">
            <li
              v-for="(article, index) in featuredGroup"
              :key="article.id"
              class="ds-list-item ds-list-item--compact"
              @click="goEdit(article.id)"
            >
              <span class="w-5 shrink-0 text-center font-metric text-body-sm font-semibold text-text-brand tabular-nums">
                {{ index + 1 }}
              </span>
              <div class="min-w-0 flex-1">
                <p class="m-0 truncate font-heading text-body-base font-medium text-text">{{ article.title }}</p>
                <p class="m-0 mt-[var(--spacer-2)] truncate text-body-xs text-text-tertiary">
                  {{ article.department_name }}
                </p>
              </div>
              <div class="flex shrink-0 items-center gap-[var(--spacer-2)]" @click.stop>
                <button
                  type="button"
                  class="ds-list-item__action-btn disabled:opacity-30"
                  aria-label="置顶上移"
                  :disabled="moving || !canMove(article, 'up')"
                  @click="moveFeatured(article, 'up')"
                >
                  <ChevronUp :size="16" />
                </button>
                <button
                  type="button"
                  class="ds-list-item__action-btn disabled:opacity-30"
                  aria-label="置顶下移"
                  :disabled="moving || !canMove(article, 'down')"
                  @click="moveFeatured(article, 'down')"
                >
                  <ChevronDown :size="16" />
                </button>
                <button
                  type="button"
                  class="ds-list-item__action-btn text-text-brand"
                  aria-label="取消置顶"
                  @click="toggleFeatured(article)"
                >
                  <Pin :size="16" fill="currentColor" />
                </button>
              </div>
            </li>
          </ul>
        </div>

        <p
          v-if="featuredGroup.length && plainArticles.length"
          class="m-0 mb-[var(--spacer-8)] text-body-xs font-medium text-text-tertiary"
        >
          全部文章
        </p>

        <!-- 紧凑行：标题 + 状态圆点 / 科室·日期·阅读量 + ⋮ -->
        <ul v-if="plainArticles.length" class="m-0 p-0 list-none flex flex-col">
          <li
            v-for="article in plainArticles"
            :key="article.id"
            class="ds-list-item"
            @click="goEdit(article.id)"
          >
            <span
              class="h-2 w-2 shrink-0 rounded-[var(--radius-full)]"
              :class="statusDotClass[article.status]"
              aria-hidden="true"
            />
            <div class="min-w-0 flex-1">
              <p class="m-0 flex items-center gap-[var(--spacer-4)] font-heading text-body-base font-medium text-text">
                <Pin
                  v-if="article.featured_rank > 0"
                  :size="12"
                  class="shrink-0 text-text-brand"
                  aria-label="已置顶"
                />
                <span class="truncate">{{ article.title }}</span>
              </p>
              <p class="m-0 mt-[var(--spacer-2)] truncate text-body-xs text-text-tertiary">
                {{ article.department_name }} · {{ metaText(article) }}
                <template v-if="article.status === 'published' && article.view_count > 0">
                  · {{ fmtCompact(article.view_count) }} 阅读
                </template>
              </p>
            </div>
            <span
              v-if="article.status !== 'published'"
              class="ds-tag ds-tag--plain shrink-0"
              :class="'ds-tag--' + statusTagType[article.status]"
            >
              {{ statusConfig[article.status].label }}
            </span>
            <button
              type="button"
              class="ds-list-item__action-btn shrink-0"
              aria-label="更多操作"
              @click.stop="openActions(article)"
            >
              <EllipsisVertical :size="16" />
            </button>
          </li>
        </ul>

        <!-- 空状态：图标 + 引导 + CTA -->
        <div
          v-if="!plainArticles.length && !featuredGroup.length"
          class="flex flex-col items-center justify-center px-[var(--spacer-32)] py-[var(--spacer-48)] text-center"
        >
          <span class="flex items-center justify-center w-14 h-14 rounded-[var(--radius-full)] bg-[var(--bg-brand-light)] text-text-brand mb-[var(--spacer-12)]">
            <FileText :size="24" />
          </span>
          <p class="m-0 font-heading text-heading-sm font-semibold text-text mb-[var(--spacer-4)]">
            {{ searchQuery || activeTab !== 'all' ? '没有匹配的文章' : '还没有文章' }}
          </p>
          <p class="m-0 text-body-sm text-text-tertiary mb-[var(--spacer-20)]">
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

        <!-- 触底加载哨兵 -->
        <div v-if="hasMore" ref="sentinel" class="h-[var(--spacer-16)]" />
        <p v-else-if="articles.length" class="m-0 py-[var(--spacer-12)] text-center text-body-xs text-text-tertiary">
          没有更多了
        </p>
        <p v-if="loadingMore" class="m-0 py-[var(--spacer-8)] text-center text-body-xs text-text-tertiary">
          加载中…
        </p>
      </template>
    </section>

    <!-- 动作面板：行内操作统一入口，删除为破坏性红色 -->
    <DsPopup :show="actionArticle !== null" position="bottom" @update:show="closeActions">
      <div class="px-[var(--spacer-16)] pb-[calc(var(--spacer-16)+env(safe-area-inset-bottom,0px))]">
        <header class="pt-[var(--spacer-16)] pb-[var(--spacer-8)]">
          <h2 class="m-0 font-heading text-heading-sm font-semibold text-text line-clamp-2">
            {{ actionArticle?.title }}
          </h2>
          <p class="m-0 mt-[var(--spacer-4)] text-body-xs text-text-tertiary">
            {{ actionArticle?.department_name }} · {{ actionArticle ? statusConfig[actionArticle.status].label : '' }}
          </p>
        </header>
        <div class="flex flex-col">
          <button
            type="button"
            class="ds-menu-row"
            @click="runSheetAction((a) => goEdit(a.id))"
          >
            <PenLine class="w-[22px] h-[22px] shrink-0 text-icon-secondary" />
            <span class="flex-1 truncate text-left text-body-base text-text">编辑</span>
          </button>
          <button
            v-if="actionArticle && canManageFeatured && actionArticle.status === 'published'"
            type="button"
            class="ds-menu-row"
            @click="runSheetAction(toggleFeatured)"
          >
            <Pin class="w-[22px] h-[22px] shrink-0" :class="actionArticle.featured_rank > 0 ? 'text-text-brand' : 'text-icon-secondary'" />
            <span class="flex-1 truncate text-left text-body-base text-text">
              {{ actionArticle.featured_rank > 0 ? '取消置顶' : '置顶' }}
            </span>
          </button>
          <button
            v-if="actionArticle?.status === 'published'"
            type="button"
            class="ds-menu-row"
            @click="runSheetAction(handleArchive)"
          >
            <Archive class="w-[22px] h-[22px] shrink-0 text-icon-secondary" />
            <span class="flex-1 truncate text-left text-body-base text-text">归档</span>
          </button>
          <button
            v-if="actionArticle?.status === 'archived'"
            type="button"
            class="ds-menu-row"
            @click="runSheetAction(handleUnarchive)"
          >
            <ArchiveRestore class="w-[22px] h-[22px] shrink-0 text-[var(--status-success-default)]" />
            <span class="flex-1 truncate text-left text-body-base text-text">取消归档</span>
          </button>
          <button
            type="button"
            class="ds-menu-row"
            @click="runSheetAction(handleDelete)"
          >
            <Trash2 class="w-[22px] h-[22px] shrink-0 text-[var(--status-error-default)]" />
            <span class="flex-1 truncate text-left text-body-base text-[var(--status-error-default)]">删除</span>
          </button>
        </div>
      </div>
    </DsPopup>
  </main>
</template>
