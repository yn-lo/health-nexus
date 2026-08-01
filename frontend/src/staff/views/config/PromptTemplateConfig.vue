<script setup lang="ts">
/**
 * 系统提示词 — 顶部展示当前生效提示词（不可删除），下方列出历史版本
 * API: configApi.getEffectivePrompt / listPromptTemplates / updatePromptTemplate
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Search, CheckCircle2, FileText, ShieldCheck, Database, Trash2 } from '@lucide/vue'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { AppHeader, StatRow } from '@/shared/components'
import { configApi, errmsg, usePagedList } from '@/shared'
import type { PromptTemplate, EffectivePromptResponse } from '@/shared'

const router = useRouter()
const { showSuccessToast, showFailToast } = useDsToast()
const { showConfirmDialog } = useDsDialog()

// ---- 当前生效提示词 ----
const effectivePrompt = ref<EffectivePromptResponse | null>(null)

async function loadEffective() {
 try {
 effectivePrompt.value = await configApi.getEffectivePrompt()
 } catch {
 // 静默失败
 }
}

// ---- 历史版本列表 ----
const search = ref('')
const filterActive = ref<'all' | 'active' | 'inactive'>('all')

const { items: templates, loading, load, onFilterChange } = usePagedList<PromptTemplate>({
 pageSize: 50,
 fetcher: (params) => configApi.listPromptTemplates({
  ...params,
  ...(filterActive.value === 'active' ? { is_active: true } : {}),
  ...(filterActive.value === 'inactive' ? { is_active: false } : {}),
 }),
})

const activeCount = computed(() => templates.value.filter(t => t.is_active).length)
const inactiveCount = computed(() => templates.value.filter(t => !t.is_active).length)

const filtered = computed(() => {
 let list = templates.value
 if (search.value.trim()) {
 const q = search.value.trim().toLowerCase()
 list = list.filter(
 (t) => t.content.toLowerCase().includes(q) || t.description.toLowerCase().includes(q),
 )
 }
 return list
})

function goDetail(t: PromptTemplate) {
 router.push({ name: 'staff-config-prompt-detail', params: { id: t.id } })
}

async function activate(t: PromptTemplate) {
 try {
 await showConfirmDialog({
 title: '设为生效版本',
 message: `将 v${t.version} 设为生效？当前生效版本将自动失活。`,
 confirmButtonText: '设为生效',
 cancelButtonText: '取消',
 })
 } catch {
 return
 }
 try {
 await configApi.updatePromptTemplate(t.id, { is_active: true })
 showSuccessToast('已生效')
 await Promise.all([loadEffective(), load()])
 } catch (e) {
 showFailToast(errmsg(e, '激活失败'))
 }
}

async function del(t: PromptTemplate) {
 const warn = t.is_active
 ? '此版本当前正在生效。删除后系统将自动使用内置默认提示词。'
 : '删除后不可恢复。'
 try {
 await showConfirmDialog({
 title: '删除提示词版本',
 message: `确认删除 v${t.version}？${warn}`,
 confirmButtonText: '删除',
 cancelButtonText: '取消',
 })
 } catch {
 return
 }
 try {
 await configApi.deletePromptTemplate(t.id)
 showSuccessToast('已删除')
 await Promise.all([loadEffective(), load()])
 } catch (e) {
 showFailToast(errmsg(e, '删除失败'))
 }
}

onMounted(() => {
 Promise.all([loadEffective(), load()])
})
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="系统提示词" @back="router.back" />

 <!-- 当前生效提示词（始终存在，不可删除） -->
 <section class="px-[var(--spacer-16)] pt-[var(--spacer-16)]">
 <div class="ds-card overflow-hidden">
 <div class="flex items-center justify-between px-[var(--spacer-16)] py-[var(--spacer-12)] border-b border-[var(--border-neutral-l1)]">
 <div class="flex items-center gap-[var(--spacer-8)]">
 <ShieldCheck :size="18" class="text-[var(--status-success-default)]" />
 <span class="font-heading text-body-base font-semibold text-text">当前生效</span>
 </div>
 <span
 v-if="effectivePrompt"
 class="ds-tag"
 :class="effectivePrompt.source === 'default' ? 'ds-tag--primary ds-tag--plain' : 'ds-tag--success ds-tag--plain'"
 >
 <Database v-if="effectivePrompt.source === 'database'" :size="10" />
 {{ effectivePrompt.source === 'default' ? '系统默认' : '自定义' }}
 </span>
 </div>
 <div class="p-[var(--spacer-16)]">
 <div
 v-if="effectivePrompt"
 class="max-h-[30vh] overflow-y-auto whitespace-pre-wrap break-words rounded-[var(--radius-8)] bg-[var(--bg-overlay-l1)] p-[var(--spacer-12)] text-body-sm text-text-secondary"
 >{{ effectivePrompt.content }}</div>
 <div v-else class="py-[var(--spacer-12)] text-center text-body-sm text-text-tertiary">加载中…</div>
 </div>
 <div v-if="effectivePrompt?.source === 'default'" class="border-t border-[var(--border-neutral-l1)] px-[var(--spacer-16)] py-[var(--spacer-8)] text-body-xs text-text-tertiary">
 此提示词由系统内置，不可删除。在数据库中创建自定义模板后，将自动覆盖此默认值。
 </div>
 </div>
 </section>

 <!-- 历史版本 -->
 <section class="px-[var(--spacer-16)] pt-[var(--spacer-24)]">
 <div class="flex items-center gap-[var(--spacer-8)] mb-[var(--spacer-8)]">
 <span class="block h-4 w-[3px] rounded-[var(--radius-sm)] bg-[var(--border-brand)]" />
 <span class="font-heading text-body-sm font-semibold text-text">版本历史</span>
 </div>

 <div class="rounded-[var(--radius-card-large)] bg-[var(--ai-gradient-soft)] px-[var(--spacer-16)] py-[var(--spacer-12)]">
 <StatRow :stats="[
 { value: templates.length, label: '总数' },
 { value: activeCount, label: '生效' },
 { value: inactiveCount, label: '未生效' },
 ]" />
 </div>

 <div class="ds-search-box mt-[var(--spacer-12)]">
 <Search class="h-4 w-4 shrink-0 text-icon-brand" />
 <input v-model="search" type="text" placeholder="搜索内容" class="min-w-0 flex-1 border-none bg-transparent font-heading text-body-base text-text outline-none placeholder:text-text-tertiary">
 </div>

 <div class="mt-[var(--spacer-8)] flex gap-[var(--spacer-24)] border-b border-[var(--border-neutral-l1)]">
 <button
 v-for="opt in [{ value: 'all', label: '全部' }, { value: 'active', label: '生效' }, { value: 'inactive', label: '未生效' }] as const"
 :key="opt.value"
 type="button"
 :class="filterActive === opt.value
 ? 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors font-medium text-text-brand'
 : 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors text-text-tertiary hover:text-text-brand'"
 @click="filterActive = opt.value; onFilterChange()"
 >{{ opt.label }}<span v-if="filterActive === opt.value" class="ds-tab-underline" /></button>
 </div>
 </section>

 <section class="px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <div v-if="filtered.length > 0" class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
 <article
 v-for="t in filtered"
 :key="t.id"
 class="ds-list-item ds-list-item--divider"
 role="link"
 tabindex="0"
 :aria-label="`v${t.version}`"
 @click="goDetail(t)"
 @keydown.enter="goDetail(t)"
 >
 <span
 class="ds-list-item__icon"
 :class="t.is_active ? 'ds-list-item__icon--success' : 'ds-list-item__icon--brand'"
 >
 <FileText :size="20" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">v{{ t.version }}</span>
 <span class="ds-list-item__meta">
 {{ t.content.slice(0, 40) }}{{ t.content.length > 40 ? '…' : '' }}
 </span>
 </div>
 <div class="ds-list-item__trailing">
 <span v-if="t.is_active" class="ds-tag ds-tag--success ds-tag--plain">
 <CheckCircle2 :size="12" />
 生效
 </span>
 <button v-if="!t.is_active" type="button" class="ds-btn ds-btn--primary ds-btn--sm" @click.stop="activate(t)">设为生效</button>
 <button type="button" class="ds-icon-btn ds-icon-btn--sm ds-icon-btn--danger ml-[var(--spacer-4)]" aria-label="删除" @click.stop="del(t)">
 <Trash2 :size="16" />
 </button>
 </div>
 </article>
 </div>
 <div v-else-if="!loading" class="flex flex-col items-center py-16">
 <span class="flex h-12 w-12 items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-overlay-l2)]">
 <FileText :size="24" class="text-icon-tertiary" />
 </span>
 <p class="mt-[var(--spacer-8)] text-body-sm text-text-tertiary">暂无历史版本</p>
 </div>
 </section>
 </main>
</template>
