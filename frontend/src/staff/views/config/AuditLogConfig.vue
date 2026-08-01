<script setup lang="ts">
/**
 * 配置审计日志 — 只读列表 + 分页 + 实体类型筛选
 * API: configApi.listAuditLogs
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ChevronLeft, ChevronRight, ClipboardList } from '@lucide/vue'
import { useDsToast } from '@/shared/composables'
import { AppHeader, StatRow } from '@/shared/components'
import { configApi } from '@/shared'
import { errmsg } from '@/shared/api/client'
import type { ConfigAuditLog, ConfigAuditEntityType } from '@/shared'

const router = useRouter()
const { showFailToast } = useDsToast()

const logs = ref<ConfigAuditLog[]>([])
const loading = ref(false)
const filterType = ref<ConfigAuditEntityType | 'all'>('all')
// entity_id 筛选：空字符串=不过滤；数字=按 ID 过滤；0=单例配置（RAG/安全话术）审计记录
const filterEntityId = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)

const typeOptions: { value: ConfigAuditEntityType | 'all'; label: string }[] = [
 { value: 'all', label: '全部' },
 { value: 'ai_provider', label: 'AI 提供商' },
 { value: 'sensitive_word', label: '敏感词' },
 { value: 'safety_rule', label: '安全规则' },
 { value: 'rag_config', label: 'RAG 配置' },
 { value: 'safety_message', label: '安全话术' },
 { value: 'prompt_template', label: 'Prompt 模板' },
]

const typeLabel: Record<ConfigAuditEntityType, string> = {
 ai_provider: 'AI 提供商',
 sensitive_word: '敏感词',
 safety_rule: '安全规则',
 rag_config: 'RAG 配置',
 safety_message: '安全话术',
 prompt_template: 'Prompt 模板',
}

const actionLabel: Record<string, string> = {
 create: '创建',
 update: '更新',
 delete: '删除',
}


async function load() {
 loading.value = true
 try {
 const params: { page: number; page_size: number; entity_type?: ConfigAuditEntityType; entity_id?: number } = {
 page: page.value,
 page_size: pageSize,
 }
 if (filterType.value !== 'all') params.entity_type = filterType.value
 const trimmedId = filterEntityId.value.trim()
 if (trimmedId !== '') {
 const id = Number(trimmedId)
 if (!Number.isNaN(id) && id >= 0) params.entity_id = id
 }
 const res = await configApi.listAuditLogs(params)
 logs.value = res.items
 total.value = res.total
 } catch (e) {
 showFailToast(errmsg(e, '加载失败'))
 } finally {
 loading.value = false
 }
}

function onFilterChange() {
 page.value = 1
 load()
}

function prevPage() {
 if (page.value > 1) {
 page.value--
 load()
 }
}

function nextPage() {
 if (page.value * pageSize < total.value) {
 page.value++
 load()
 }
}

const computedTotalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

const totalCount = computed(() => total.value)

function fmtDateTime(s: string): string {
 return s ? s.slice(0, 19).replace('T', ' ') : '--'
}

function fmtChanges(changes: Record<string, unknown> | null): string {
 if (!changes) return ''
 return Object.entries(changes)
 .map(([k, v]) => `${k}: ${JSON.stringify(v)}`)
 .join(', ')
}

onMounted(load)
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="审计日志" @back="router.back" />

 <!-- 筛选 -->
 <section class="px-[var(--spacer-16)] pt-[var(--spacer-12)] pb-[var(--spacer-8)]">
 <div class="rounded-[var(--radius-card-large)] bg-[var(--ai-gradient-soft)] px-[var(--spacer-16)] py-[var(--spacer-12)]">
 <StatRow :stats="[
 { value: totalCount, label: '日志总数' },
 ]" />
 </div>

 <div class="flex gap-[var(--spacer-24)] border-b border-[var(--border-neutral-l1)] no-scrollbar overflow-x-auto mt-[var(--spacer-12)]">
 <button
 v-for="opt in typeOptions"
 :key="opt.value"
 type="button"
 :class="filterType === opt.value
 ? 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors font-medium text-text-brand'
 : 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors text-text-tertiary hover:text-text-brand'"
 @click="filterType = opt.value; onFilterChange()"
 >{{ opt.label }}<span v-if="opt.value === 'all' && totalCount > 0" class="ml-[var(--spacer-4)] inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)] px-[var(--spacer-4)] text-[10px] font-medium text-text-brand">{{ totalCount }}</span><span v-if="filterType === opt.value" class="ds-tab-underline" /></button>
 </div>
 <!-- entity_id 筛选：留空=全部；填数字=按 ID；0=单例配置审计记录 -->
 <div class="mt-[var(--spacer-12)] flex items-center gap-[var(--spacer-8)]">
 <span class="text-body-sm text-text-secondary">实体 ID</span>
 <input
 v-model="filterEntityId"
 type="text"
 inputmode="numeric"
 pattern="[0-9]*"
 placeholder="留空=全部，0=单例配置"
 class="ds-input ds-input--sm flex-1"
 @keyup.enter="onFilterChange"
 >
 <button type="button" class="ds-btn ds-btn--secondary ds-btn--sm" @click="onFilterChange">筛选</button>
 </div>
 </section>

 <!-- 列表 -->
 <section class="px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <div v-if="logs.length > 0" class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
 <article
 v-for="log in logs"
 :key="log.id"
 class="ds-list-item ds-list-item--divider"
 >
 <span
 class="ds-list-item__icon"
 :class="log.action === 'delete' ? 'ds-list-item__icon--error' : log.action === 'update' ? 'ds-list-item__icon--alert' : log.action === 'create' ? 'ds-list-item__icon--success' : ''"
 >
 <ClipboardList :size="20" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">{{ actionLabel[log.action] ?? log.action }} · {{ typeLabel[log.entity_type] ?? log.entity_type }} <span v-if="log.entity_id"> #{{ log.entity_id }}</span></span>
 <span class="ds-list-item__meta">
 <span>操作者 #{{ log.operator_id }} ({{ log.operator_role }})</span>
 <span v-if="log.changes && Object.keys(log.changes).length > 0">· {{ fmtChanges(log.changes).slice(0, 50) }}{{ fmtChanges(log.changes).length > 50 ? '…' : '' }}</span>
 </span>
 </div>
 <span class="ds-list-item__time">{{ fmtDateTime(log.created_at) }}</span>
 </article>
 </div>
 <div v-else-if="!loading" class="flex flex-col items-center py-20">
 <span class="flex h-14 w-14 items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)]">
 <ClipboardList :size="28" class="text-icon-brand" />
 </span>
 <p class="mt-[var(--spacer-12)] text-heading-sm font-semibold text-text">暂无审计日志</p>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-tertiary">系统配置变更后将自动记录</p>
 </div>
 </section>

 <!-- 分页 -->
 <section v-if="total > pageSize" class="flex items-center justify-center gap-[var(--spacer-16)] px-[var(--spacer-16)] py-[var(--spacer-12)]">
 <button type="button" class="ds-btn ds-btn--secondary ds-btn--sm" :disabled="page <= 1" @click="prevPage">
 <ChevronLeft class="h-4 w-4" />
 </button>
 <span class="text-body-sm text-text-secondary">
 {{ page }} / {{ computedTotalPages }}
 </span>
 <button type="button" class="ds-btn ds-btn--secondary ds-btn--sm" :disabled="page >= computedTotalPages" @click="nextPage">
 <ChevronRight class="h-4 w-4" />
 </button>
 </section>
 </main>
</template>
