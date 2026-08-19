<script setup lang="ts">
/**
 * CrisisEventList 危机事件列表 — 医护端
 * 对应需求: REQ-CHAT-015 / REQ-CHAT-016
 * 后端 API: GET /api/staff/chat/crisis-events, POST /api/staff/chat/crisis-events/{id}/handle
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { AlertOctagon, AlertTriangle, ShieldAlert, CheckCircle2, Search } from '@lucide/vue'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { AppHeader, PageShell, EmptyState } from '@/shared/components'
import { staffChatApi } from '@/shared'
import { errmsg } from '@/shared/api/client'
import type { CrisisEventItem, CrisisLevel } from '@/shared'

const router = useRouter()
const { showSuccessToast, showFailToast } = useDsToast()
const { showConfirmDialog } = useDsDialog()

const events = ref<CrisisEventItem[]>([])
const loading = ref(false)
const search = ref('')

/** 过滤状态: all / unresolved / resolved */
const filterStatus = ref<'all' | 'unresolved' | 'resolved'>('unresolved')

/** 过滤级别: all / high / medium / low */
const filterLevel = ref<'all' | CrisisLevel>('all')

/** 级别 → 中文标签 */
function levelLabel(level: CrisisLevel): string {
 if (level === 'high') return '危机'
 if (level === 'medium') return '紧急'
 return '一般'
}

/** 级别 → ds-tag 变体 */
function levelTagType(level: CrisisLevel): 'danger' | 'warning' | 'primary' {
 if (level === 'high') return 'danger'
 if (level === 'medium') return 'warning'
 return 'primary'
}

/** 过滤后的事件列表 */
const filteredEvents = computed(() => {
 let list = events.value
 if (filterStatus.value === 'unresolved') {
 list = list.filter((e) => !e.handled)
 } else if (filterStatus.value === 'resolved') {
 list = list.filter((e) => e.handled)
 }
 if (filterLevel.value !== 'all') {
 list = list.filter((e) => e.level === filterLevel.value)
 }
 if (search.value.trim()) {
 const kw = search.value.trim().toLowerCase()
 list = list.filter(
 (e) =>
 e.triggered_content.toLowerCase().includes(kw) ||
 e.patient_name.toLowerCase().includes(kw) ||
 e.matched_keywords.some((k) => k.toLowerCase().includes(kw)),
 )
 }
 return list
})

/** 统计 */
const unresolvedCount = computed(() => events.value.filter((e) => !e.handled).length)
const crisisCount = computed(() => events.value.filter((e) => e.level === 'high' && !e.handled).length)

/** 格式化日期时间 */
function fmtDateTime(dateStr: string | null): string {
 if (!dateStr) return '--'
 return dateStr.slice(0, 16).replace('T', ' ')
}

/** 加载危机事件列表 */
async function loadEvents() {
 loading.value = true
 try {
 const res = await staffChatApi.listCrisisEvents({ page: 1, page_size: 100 })
 events.value = res.items
 } catch (e) {
 showFailToast(errmsg(e, '加载失败'))
 } finally {
 loading.value = false
 }
}

/** 标记危机事件已处理 */
async function handleResolve(event: CrisisEventItem) {
 try {
 await showConfirmDialog({
 title: '确认处理',
 message: `确认将此危机事件标记为已处理？\n\n患者：${event.patient_name}\n触发内容：${event.triggered_content.slice(0, 50)}...`,
 })
 } catch {
 return // 用户取消
 }
 try {
 await staffChatApi.handleCrisisEvent(event.id, { note: '' })
 showSuccessToast('已标记为已处理')
 await loadEvents()
 } catch (e) {
 showFailToast(errmsg(e, '处理失败'))
 }
}

onMounted(loadEvents)
</script>

<template>
 <PageShell :bottom-nav="false">
 <AppHeader title="危机事件" @back="router.back" />

 <!-- 统计概览 — 圆形图标徽章 + 大号 metric 数字 + 小标签，比例均衡 -->
 <div class="px-[var(--spacer-16)] pt-[var(--spacer-16)]">
 <div class="grid grid-cols-2 gap-[var(--spacer-12)]">
 <div class="flex items-center gap-[var(--spacer-12)] rounded-[var(--radius-card-medium)] bg-[var(--bg-brand-light)] p-[var(--spacer-16)]">
 <span class="inline-flex items-center justify-center shrink-0 w-9 h-9 rounded-full bg-[var(--bg-brand)]">
 <AlertOctagon class="w-5 h-5 text-icon-onbrand" />
 </span>
 <div class="min-w-0">
 <div class="font-metric text-heading-xl leading-none font-semibold text-text-brand tabular-nums">{{ unresolvedCount }}</div>
 <div class="mt-[var(--spacer-4)] text-body-sm text-text-secondary">未处理</div>
 </div>
 </div>
 <div class="flex items-center gap-[var(--spacer-12)] rounded-[var(--radius-card-medium)] bg-[var(--bg-brand)] p-[var(--spacer-16)] shadow-[var(--shadow-brand)]">
 <span class="inline-flex items-center justify-center shrink-0 w-9 h-9 rounded-full bg-[var(--text-onbrand)]">
 <ShieldAlert class="w-5 h-5 text-icon-brand" />
 </span>
 <div class="min-w-0">
 <div class="font-metric text-heading-xl leading-none font-semibold text-onbrand tabular-nums">{{ crisisCount }}</div>
 <div class="mt-[var(--spacer-4)] text-body-sm text-onbrand opacity-75">危机级</div>
 </div>
 </div>
 </div>
 </div>

 <!-- 搜索 + 筛选 -->
 <section class="px-[var(--spacer-16)] pt-[var(--spacer-16)]">
 <div class="ds-search-box ds-search-box--md">
 <Search class="ds-search-box__icon h-4 w-4" />
 <input
 v-model="search"
 type="text"
 placeholder="搜索触发内容/患者/关键词"
 class="ds-search-box__input"
 >
 </div>

 <!-- 状态筛选 -->
 <div class="mt-[var(--spacer-12)]">
  <select v-model="filterStatus" class="ds-input">
   <option value="unresolved">未处理</option>
   <option value="resolved">已处理</option>
   <option value="all">全部</option>
  </select>
 </div>

 <!-- 级别筛选 — chip 胶囊组（次级筛选，与状态 Tab 形成层级） -->
 <div class="flex flex-wrap gap-[var(--spacer-8)] mt-[var(--spacer-12)]">
 <button
 v-for="opt in [
 { value: 'all', label: '全部级别' },
 { value: 'high', label: '危机' },
 { value: 'medium', label: '紧急' },
 { value: 'low', label: '一般' },
 ] as const"
 :key="opt.value"
 type="button"
 class="inline-flex items-center h-8 px-[var(--spacer-12)] rounded-[var(--radius-full)] border text-body-sm font-medium whitespace-nowrap transition-colors"
 :class="filterLevel === opt.value
 ? 'border-[var(--brand-glow-border-strong)] bg-[var(--bg-brand-light)] text-text-brand'
 : 'border-[var(--border-neutral-l1)] bg-transparent text-text-secondary hover:bg-[var(--bg-overlay-l1)] hover:text-text'"
 @click="filterLevel = opt.value"
 >{{ opt.label }}</button>
 </div>
 </section>

 <!-- 事件列表 -->
 <section class="px-[var(--spacer-16)] pt-[var(--spacer-16)] pb-[var(--spacer-16)]">
 <div v-if="filteredEvents.length === 0 && loading" class="py-[var(--spacer-32)] text-center text-body-md text-text-tertiary">
 加载中...
 </div>
 <EmptyState v-else-if="filteredEvents.length === 0" text="暂无危机事件" />
 <div v-else class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
 <article
 v-for="event in filteredEvents"
 :key="event.id"
 class="ds-list-item ds-list-item--divider"
 >
 <span
 class="ds-list-item__icon"
 :class="event.handled
 ? 'ds-list-item__icon--success'
 : event.level === 'high'
 ? 'ds-list-item__icon--error'
 : 'ds-list-item__icon--alert'"
 >
 <AlertTriangle :size="20" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">{{ event.triggered_content.slice(0, 40) }}{{ event.triggered_content.length > 40 ? '…' : '' }}</span>
 <span class="ds-list-item__meta">
 <span class="ds-tag ds-tag--plain" :class="'ds-tag--' + levelTagType(event.level)">{{ levelLabel(event.level) }}</span>
 <span>· #{{ event.id }}</span>
 <span v-if="event.patient_name">· {{ event.patient_name }}</span>
 <span>· {{ fmtDateTime(event.created_at) }}</span>
 </span>
 </div>
 <div class="ds-list-item__trailing">
 <span v-if="event.handled" class="ds-tag ds-tag--success ds-tag--plain">已处理</span>
 <span v-else class="ds-tag ds-tag--primary">待处理</span>
 <button
 v-if="!event.handled"
 type="button"
 class="ds-list-item__action-btn"
 aria-label="标记已处理"
 @click="handleResolve(event)"
 >
 <CheckCircle2 :size="16" />
 </button>
 </div>
 </article>
 </div>
 </section>
 </PageShell>
</template>
