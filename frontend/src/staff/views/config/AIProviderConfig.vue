<script setup lang="ts">
/**
 * AI 提供商配置 — 列表
 * 展示方式：按能力（LLM / Embedding / Rerank）分组，组内按 priority 呈现主备链。
 * API: configApi.listAIProviders/updateAIProvider/deleteAIProvider
 */
import { ref, computed, onMounted } from 'vue'
import type { Component } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Search, Brain, Layers, ArrowUpDown, Plug, Info } from '@lucide/vue'
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

const PROVIDER_TYPES: AIProviderType[] = ['llm', 'embedding', 'rerank']

const typeLabel: Record<AIProviderType, string> = {
  llm: 'LLM',
  embedding: 'Embedding',
  rerank: 'Rerank',
}

const typeIcon: Record<AIProviderType, Component> = {
  llm: Brain,
  embedding: Layers,
  rerank: ArrowUpDown,
}

type ChainRole = 'main' | 'backup' | 'inactive'

interface ChainItem {
  provider: AIProvider
  role: ChainRole
  backupOrder: number
}

interface ProviderSection {
  type: AIProviderType
  label: string
  icon: Component
  activeCount: number
  items: ChainItem[]
}

const heroStats = computed(() =>
  PROVIDER_TYPES.map((type) => ({
    value: providers.value.filter((p) => p.provider_type === type && p.is_active).length,
    label: typeLabel[type],
  })),
)

const matched = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return providers.value
  return providers.value.filter(
    (p) => p.name.toLowerCase().includes(q) || p.model_name.toLowerCase().includes(q),
  )
})

/** 按能力分组：组内先按 priority 排列主备链，再列出未启用项。 */
const sections = computed<ProviderSection[]>(() =>
  PROVIDER_TYPES.map((type) => {
    const list = matched.value.filter((p) => p.provider_type === type)
    const active = list.filter((p) => p.is_active)
    const items: ChainItem[] = active.map((provider, i) => ({
      provider,
      role: i === 0 ? 'main' : 'backup',
      backupOrder: i,
    }))
    for (const provider of list.filter((p) => !p.is_active)) {
      items.push({ provider, role: 'inactive', backupOrder: 0 })
    }
    return { type, label: typeLabel[type], icon: typeIcon[type], activeCount: active.length, items }
  }).filter((section) => section.items.length > 0),
)

function roleLabel(item: ChainItem): string {
  if (item.role === 'main') return '主用'
  if (item.role === 'backup') return `备用 ${item.backupOrder}`
  return '未启用'
}

function roleClass(item: ChainItem): string {
  return item.role === 'main' ? 'ds-tag ds-tag--primary' : 'ds-tag ds-tag--default'
}

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

    <!-- Hero 统计卡片：各能力的生效 provider 数 -->
    <section v-if="providers.length > 0" class="mx-[var(--spacer-16)] mt-[var(--spacer-8)] rounded-[var(--radius-card-large)] bg-[var(--ai-gradient-soft)] px-[var(--spacer-16)] py-[var(--spacer-16)]">
      <StatRow :stats="heroStats" />
    </section>

    <!-- 主备链说明 -->
    <section v-if="providers.length > 0" class="mx-[var(--spacer-16)] mt-[var(--spacer-12)]">
      <div class="flex gap-[var(--spacer-8)] rounded-[var(--radius-8)] bg-[var(--bg-brand-light)] px-[var(--spacer-12)] py-[var(--spacer-8)]">
        <Info :size="16" class="mt-[2px] shrink-0 text-icon-brand" />
        <p class="text-body-xs text-text-secondary">
          同类 provider 按优先级组成主备链：主用失败自动切换备用；回答已开始输出后不再切换。
        </p>
      </div>
    </section>

    <!-- 搜索 -->
    <section class="px-[var(--spacer-16)] pt-[var(--spacer-8)] pb-[var(--spacer-8)]">
      <div class="ds-search-box">
        <Search class="h-4 w-4 shrink-0 text-icon-brand" />
        <input v-model="search" type="text" placeholder="搜索名称或模型" class="min-w-0 flex-1 border-none bg-transparent font-heading text-body-base text-text outline-none placeholder:text-text-tertiary">
      </div>
    </section>

    <!-- 分组主备链 -->
    <section v-if="sections.length > 0" class="flex flex-col gap-[var(--spacer-20)] px-[var(--spacer-16)] pb-[var(--spacer-8)]">
      <div v-for="s in sections" :key="s.type">
        <div class="mb-[var(--spacer-8)] flex items-center justify-between px-[var(--spacer-4)]">
          <h2 class="flex items-center gap-[var(--spacer-6)] text-body-sm font-medium text-text-secondary">
            <component :is="s.icon" :size="14" class="text-icon-brand" />
            {{ s.label }}
          </h2>
          <span
            class="text-body-xs"
            :class="s.activeCount > 0 ? 'text-text-tertiary' : 'text-[var(--status-warning-default)]'"
          >{{ s.activeCount > 0 ? `${s.activeCount} 条生效` : '无生效 provider' }}</span>
        </div>

        <div class="ds-list overflow-hidden rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)]">
          <article
            v-for="item in s.items"
            :key="item.provider.id"
            class="ds-list-item ds-list-item--divider cursor-pointer"
            :class="{ 'opacity-60': item.role === 'inactive' }"
            @click="router.push({ name: 'staff-config-ai-provider-edit', params: { id: item.provider.id } })"
          >
            <span
              class="ds-list-item__icon"
              :class="item.role === 'main' ? 'ds-list-item__icon--brand' : item.role === 'backup' ? 'ds-list-item__icon--success' : ''"
            >
              <component :is="s.icon" :size="20" />
            </span>
            <div class="ds-list-item__content">
              <div class="ds-list-item__title flex items-center gap-[var(--spacer-6)]">
                <span class="min-w-0 flex-1 truncate">{{ item.provider.name }}</span>
                <span class="shrink-0" :class="roleClass(item)">{{ roleLabel(item) }}</span>
              </div>
              <span class="ds-list-item__meta">
                <span class="truncate">{{ item.provider.model_name }}</span>
                <span v-if="item.provider.dimension">· dim {{ item.provider.dimension }}</span>
                <span v-if="item.role !== 'main'">· 优先级 {{ item.provider.priority }}</span>
              </span>
            </div>
            <div class="ds-list-item__trailing">
              <button
                type="button"
                class="ds-list-item__action-btn"
                aria-label="测试连通性"
                :disabled="testingId === item.provider.id"
                @click.stop="testConnectivity(item.provider)"
              >
                <Plug
                  :size="16"
                  :class="{
                    'animate-spin': testingId === item.provider.id,
                    'text-[var(--status-success-default)]': testedIds.has(item.provider.id),
                  }"
                />
              </button>
              <label class="ds-switch ds-switch--sm">
                <input type="checkbox" class="ds-switch__input" :checked="item.provider.is_active" @change="toggleActive(item.provider)">
                <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
              </label>
            </div>
          </article>
        </div>
      </div>
    </section>

    <!-- 空状态 -->
    <section v-else-if="!loading" class="px-[var(--spacer-16)] py-[var(--spacer-8)]">
      <div class="flex flex-col items-center py-[var(--spacer-48)]">
        <div class="flex h-[var(--icon-size-48)] w-[var(--icon-size-48)] items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)]">
          <Brain :size="24" class="text-icon-brand" />
        </div>
        <template v-if="providers.length === 0">
          <p class="mt-[var(--spacer-16)] text-body-base font-medium text-text">还没有 AI 提供商</p>
          <p class="mt-[var(--spacer-4)] text-body-sm text-text-tertiary">添加第一个 AI 提供商</p>
          <button
            type="button"
            class="ds-btn ds-btn--primary ds-btn--sm mt-[var(--spacer-16)]"
            @click="router.push({ name: 'staff-config-ai-provider-create' })"
          >新建</button>
        </template>
        <p v-else class="mt-[var(--spacer-16)] text-body-base font-medium text-text">没有匹配的 provider</p>
      </div>
    </section>
  </main>
</template>
