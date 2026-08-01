<script setup lang="ts">
/**
 * AI 提供商配置 — 列表
 * API: configApi.listAIProviders/updateAIProvider/deleteAIProvider
 */
import { ref, computed, onMounted } from 'vue'
import type { Component } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Search, Brain, Layers, ArrowUpDown, PenLine, Plug } from '@lucide/vue'
import { useDsToast } from '@/shared/composables'
import { AppHeader, StatRow } from '@/shared/components'
import { configApi } from '@/shared'
import { errmsg } from '@/shared/api/client'
import type { AIProvider, AIProviderType } from '@/shared'

const router = useRouter()
const { showSuccessToast, showFailToast } = useDsToast()

const providers = ref<AIProvider[]>([])
const loading = ref(false)
const search = ref('')
const filterType = ref<AIProviderType | 'all'>('all')

const typeOptions: { value: AIProviderType | 'all'; label: string }[] = [
 { value: 'all', label: '全部' },
 { value: 'llm', label: 'LLM' },
 { value: 'embedding', label: 'Embedding' },
 { value: 'rerank', label: 'Rerank' },
 { value: 'rewrite', label: 'Rewrite' },
]

const typeLabel: Record<AIProviderType, string> = {
 llm: 'LLM',
 embedding: 'Embedding',
 rerank: 'Rerank',
 rewrite: 'Rewrite',
}

const typeIcon: Record<AIProviderType, Component> = {
 llm: Brain,
 embedding: Layers,
 rerank: ArrowUpDown,
 rewrite: PenLine,
}

const typeCounts = computed<Record<string, number>>(() => {
 const counts: Record<string, number> = { all: providers.value.length }
 for (const t of ['llm', 'embedding', 'rerank', 'rewrite'] as AIProviderType[]) {
 counts[t] = providers.value.filter(p => p.provider_type === t).length
 }
 return counts
})

const heroStats = computed(() => [
 { value: providers.value.length, label: '总数' },
 { value: providers.value.filter(p => p.is_active).length, label: '已启用' },
 { value: providers.value.filter(p => !p.is_active).length, label: '未启用' },
])

const filtered = computed(() => {
 let list = providers.value
 if (filterType.value !== 'all') {
 list = list.filter((p) => p.provider_type === filterType.value)
 }
 if (search.value.trim()) {
 const q = search.value.trim().toLowerCase()
 list = list.filter(
 (p) => p.name.toLowerCase().includes(q) || p.model_name.toLowerCase().includes(q),
 )
 }
 return list
})

async function load() {
 loading.value = true
 try {
 providers.value = await configApi.listAIProviders()
 } catch (e) {
 showFailToast(errmsg(e, '加载失败'))
 } finally {
 loading.value = false
 }
}

async function toggleActive(p: AIProvider) {
 try {
 await configApi.updateAIProvider(p.id, { is_active: !p.is_active })
 p.is_active = !p.is_active
 } catch (e) {
 showFailToast(errmsg(e, '切换失败'))
 }
}

const testingId = ref<number | null>(null)
const testedIds = ref<Set<number>>(new Set())

async function testConnectivity(p: AIProvider) {
 testingId.value = p.id
 try {
 const res = await configApi.testAIProvider(p.id)
 if (res.success) {
 showSuccessToast(`${res.detail}（${res.latency_ms}ms）`)
 testedIds.value.add(p.id)
 } else {
 showFailToast(res.detail)
 testedIds.value.delete(p.id)
 }
 } catch (e) {
 showFailToast(errmsg(e, '测试失败'))
 testedIds.value.delete(p.id)
 } finally {
 testingId.value = null
 }
}

onMounted(load)
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="AI 提供商" @back="router.back">
 <template #right>
 <button
 type="button"
 class="ds-icon-btn ds-icon-btn--sm ds-icon-btn--brand"
 aria-label="新增"
 @click="router.push({ name: 'staff-config-ai-provider-create' })"
 >
 <Plus class="icon h-5 w-5" />
 </button>
 </template>
 </AppHeader>

 <!-- Hero 统计卡片 -->
 <section v-if="providers.length > 0" class="mx-[var(--spacer-16)] mt-[var(--spacer-8)] rounded-[var(--radius-card-large)] bg-[var(--ai-gradient-soft)] px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <StatRow :stats="heroStats" />
 </section>

 <!-- 搜索 + 类型筛选 -->
 <section class="px-[var(--spacer-16)] pt-[var(--spacer-8)] pb-[var(--spacer-8)]">
 <div class="ds-search-box">
 <Search class="h-4 w-4 shrink-0 text-icon-brand" />
 <input v-model="search" type="text" placeholder="搜索名称或模型" class="min-w-0 flex-1 border-none bg-transparent font-heading text-body-base text-text outline-none placeholder:text-text-tertiary">
 </div>
 <div class="no-scrollbar mt-[var(--spacer-12)] flex gap-[var(--spacer-24)] overflow-x-auto border-b border-[var(--border-neutral-l1)] pl-[var(--spacer-8)]">
 <button
 v-for="opt in typeOptions"
 :key="opt.value"
 type="button"
 class="relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors"
 :class="filterType === opt.value ? 'font-medium text-text-brand' : 'text-text-tertiary hover:text-text-brand'"
 @click="filterType = opt.value"
 >{{ opt.label }}<span v-if="typeCounts[opt.value] !== undefined && typeCounts[opt.value] > 0" class="ml-[var(--spacer-4)] inline-flex items-center justify-center min-w-[16px] h-[16px] px-[var(--spacer-4)] rounded-[var(--radius-full)] text-[10px] font-medium leading-none transition-colors" :class="filterType === opt.value ? 'bg-[var(--bg-brand-light)] text-text-brand' : 'bg-[var(--bg-overlay-l1)] text-text-tertiary'">{{ typeCounts[opt.value] }}</span><span v-if="filterType === opt.value" class="ds-tab-underline" /></button>
 </div>
 </section>

 <!-- 列表 -->
 <section class="px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <div v-if="filtered.length > 0" class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
 <article
 v-for="p in filtered"
 :key="p.id"
 class="ds-list-item ds-list-item--divider cursor-pointer"
 @click="router.push({ name: 'staff-config-ai-provider-edit', params: { id: p.id } })"
 >
 <span
 class="ds-list-item__icon"
 :class="p.is_active ? 'ds-list-item__icon--brand' : ''"
 >
 <component :is="typeIcon[p.provider_type]" :size="20" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">{{ p.name }}</span>
 <span class="ds-list-item__meta">
 <span>{{ typeLabel[p.provider_type] }}</span>
 <span>· {{ p.model_name }}</span>
 <span v-if="p.dimension">· dim {{ p.dimension }}</span>
 </span>
 </div>
 <div class="ds-list-item__trailing">
 <button
 type="button"
 class="ds-list-item__action-btn"
 aria-label="测试连通性"
 :disabled="testingId === p.id"
 @click.stop="testConnectivity(p)"
 >
 <Plug
 :size="16"
 :class="{
 'animate-spin': testingId === p.id,
 'text-[var(--status-success-default)]': testedIds.has(p.id),
 }"
 />
 </button>
 <label class="ds-switch ds-switch--sm">
 <input type="checkbox" class="ds-switch__input" :checked="p.is_active" @change="toggleActive(p)">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 </div>
 </article>
 </div>
 <!-- 增强空状态 -->
 <div v-else-if="!loading" class="flex flex-col items-center py-[var(--spacer-48)]">
 <div class="flex h-[var(--icon-size-48)] w-[var(--icon-size-48)] items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)]">
 <Brain :size="24" class="text-icon-brand" />
 </div>
 <p class="mt-[var(--spacer-16)] text-body-base font-medium text-text">还没有 AI 提供商</p>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-tertiary">添加第一个 AI 提供商</p>
 <button
 type="button"
 class="ds-btn ds-btn--primary ds-btn--sm mt-[var(--spacer-16)]"
 @click="router.push({ name: 'staff-config-ai-provider-create' })"
 >新建</button>
 </div>
 </section>
 </main>
</template>
