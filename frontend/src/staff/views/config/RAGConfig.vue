<script setup lang="ts">
/**
 * RAG 参数配置 — 单例 GET/PUT
 * API: configApi.getRAGConfig/updateRAGConfig
 * 范围校验：与后端 entity.RAG_LIMITS 一致
 */
import { ref, computed, onMounted, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { useDsToast } from '@/shared/composables'
import { AppHeader } from '@/shared/components'
import { configApi } from '@/shared'
import { errmsg } from '@/shared/api/client'
import { RAG_LIMITS } from '@/shared/types/config'
import type { RAGConfig as RAGConfigData, RAGConfigUpdateRequest } from '@/shared'

const router = useRouter()
const { showSuccessToast, showFailToast } = useDsToast()

const loading = ref(false)
const saving = ref(false)
const updated_at = ref('')

const form = reactive({
 chunk_size: 500,
 chunk_overlap: 50,
 max_chunks: 10,
 top_k: 5,
 similarity_threshold: 0.75,
 rerank_enabled: true,
 rerank_threshold: 0.5,
 ood_threshold: 0.3,
})

const chunkFields: { key: keyof Pick<RAGConfigUpdateRequest, 'chunk_size' | 'chunk_overlap' | 'max_chunks'>; label: string; step: number; isFloat: boolean; hint: string }[] = [
 { key: 'chunk_size', label: '切片大小', step: 100, isFloat: false, hint: '每段切片的最大字符数。推荐 500。医疗文档建议 300-800，过长导致语义稀释，过短丢失上下文。' },
 { key: 'chunk_overlap', label: '切片重叠', step: 10, isFloat: false, hint: '相邻切片重叠的字符数。推荐 50。一般为切片大小的 10%，避免切断关键信息。' },
 { key: 'max_chunks', label: '最大切片数', step: 1, isFloat: false, hint: '单次检索返回的最大切片数。推荐 10。医疗场景建议 5-15，过多引入噪声，过少遗漏信息。' },
]

const retrievalFields: { key: keyof Pick<RAGConfigUpdateRequest, 'top_k' | 'similarity_threshold' | 'rerank_threshold' | 'ood_threshold'>; label: string; step: number; isFloat: boolean; hint: string }[] = [
 { key: 'top_k', label: '检索数量 (top_k)', step: 1, isFloat: false, hint: '向量+BM25 混合检索的候选数。推荐 5。医疗场景建议 3-10，影响召回率和性能。' },
 { key: 'similarity_threshold', label: '相似度阈值', step: 0.05, isFloat: true, hint: '向量相似度过滤阈值 (0-1)。推荐 0.75。低于此值的切片不被采用。医疗场景建议 0.7-0.85，过高导致漏答，过低导致误答。设为 0 表示不过滤（不推荐）。' },
 { key: 'rerank_threshold', label: 'Rerank 阈值', step: 0.05, isFloat: true, hint: 'Rerank 重排后的过滤阈值。推荐 0.5。低于此值的切片在 rerank 后被剔除。仅在启用 Rerank 时生效。' },
 { key: 'ood_threshold', label: 'OOD 阈值', step: 0.05, isFloat: true, hint: '知识库外检测阈值 (0-0.5)。所有切片最大向量相似度低于此值时系统拒答。推荐 0.3。设为 0 关闭检测。过高可能导致正常问答被拒。' },
]

const allNumericFields = [...chunkFields, ...retrievalFields]

/** 分组渲染配置（合并结构相同的两块，消除模板重复） */
const fieldSections = [
 { title: '切片配置', fields: chunkFields },
 { title: '检索配置', fields: retrievalFields },
]

function rangeHint(key: keyof typeof RAG_LIMITS): string {
 const r = RAG_LIMITS[key]
 return `${r.min} ~ ${r.max}`
}

function validateField(key: keyof typeof RAG_LIMITS, value: number): string | null {
 const r = RAG_LIMITS[key]
 if (value < r.min || value > r.max) return `范围 ${r.min} ~ ${r.max}`
 return null
}

const errors = computed<Record<string, string>>(() => {
 const e: Record<string, string> = {}
 for (const f of allNumericFields) {
 const v = form[f.key] as number
 const err = validateField(f.key, v)
 if (err) e[f.key] = err
 }
 if (form.chunk_overlap >= form.chunk_size) {
 e.chunk_overlap = '必须小于切片大小'
 }
 return e
})

const hasError = computed(() => Object.keys(errors.value).length > 0)

async function load() {
 loading.value = true
 try {
 const c: RAGConfigData = await configApi.getRAGConfig()
 form.chunk_size = c.chunk_size
 form.chunk_overlap = c.chunk_overlap
 form.max_chunks = c.max_chunks
 form.top_k = c.top_k
 form.similarity_threshold = c.similarity_threshold
 form.rerank_enabled = c.rerank_enabled
 form.rerank_threshold = c.rerank_threshold
 form.ood_threshold = c.ood_threshold
 updated_at.value = c.updated_at
 } catch (e) {
 showFailToast(errmsg(e, '加载失败'))
 } finally {
 loading.value = false
 }
}

async function save() {
 if (hasError.value) {
 showFailToast('请先修正范围错误')
 return
 }
 saving.value = true
 try {
 const patch: RAGConfigUpdateRequest = { ...form }
 const c = await configApi.updateRAGConfig(patch)
 updated_at.value = c.updated_at
 showSuccessToast('已保存')
 } catch (e) {
 showFailToast(errmsg(e, '保存失败'))
 } finally {
 saving.value = false
 }
}

onMounted(load)
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="RAG 参数" @back="router.back" />

 <section class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <div class="mb-[var(--spacer-16)] rounded-[var(--radius-card-soft)] bg-[var(--ai-gradient-soft)] p-[var(--spacer-16)]">
 <h2 class="text-[var(--body-lg-strong-font-size)] font-semibold text-text">RAG 参数配置</h2>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-secondary">单例配置，影响检索与重排行为。所有字段范围与后端 CHECK 约束一致。</p>
 <p class="mt-[var(--spacer-8)] text-body-sm text-[var(--status-warning-default)]">⚠️ 医疗场景推荐配置：相似度阈值 ≥ 0.75，启用 Rerank。不合理配置可能导致误答或漏答。</p>
 </div>

 <div class="flex flex-col gap-[var(--spacer-16)]">
 <div v-for="section in fieldSections" :key="section.title">
 <div class="text-body-sm font-medium text-text-secondary mb-[var(--spacer-8)] pl-[var(--spacer-12)] border-l-2 border-[var(--border-brand)]">{{ section.title }}</div>
 <div class="flex flex-col gap-[var(--spacer-12)]">
 <div
 v-for="f in section.fields"
 :key="f.key"
 class="rounded-[var(--radius-card-soft)] border border-[var(--border-brand)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]"
 >
 <div class="mb-[var(--spacer-4)] flex items-center gap-[var(--spacer-8)]">
 <span class="text-body-sm text-text-secondary">{{ f.label }}</span>
 <span class="ds-pill ds-pill--sm">{{ rangeHint(f.key) }}</span>
 </div>
 <input
 v-model.number="form[f.key]"
 :type="f.isFloat ? 'number' : 'digit'"
 :step="f.step"
 inputmode="decimal"
 class="ds-input ds-input--secondary"
 >
 <p class="mt-[var(--spacer-4)] text-body-xs text-text-tertiary leading-relaxed">{{ f.hint }}</p>
 <p v-if="errors[f.key]" class="mt-[var(--spacer-4)] text-body-xs text-[var(--status-error-default)]">{{ errors[f.key] }}</p>
 </div>
 </div>
 </div>

 <div class="flex items-center justify-between rounded-[var(--radius-card-soft)] border border-[var(--border-brand)] bg-[var(--ai-gradient-soft)] p-[var(--spacer-16)]">
 <div>
 <div class="text-body-base-strong font-medium text-text">启用 Rerank</div>
 <div class="mt-[var(--spacer-4)] text-body-sm text-text-tertiary">开启后对检索结果进行二次重排，显著提升精度。医疗场景强烈建议开启。未配置 Rerank API 时自动降级跳过，不影响系统运行。</div>
 </div>
 <label class="ds-switch">
 <input type="checkbox" class="ds-switch__input" v-model="form.rerank_enabled">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 </div>
 </div>

 <div class="mt-[var(--spacer-24)] flex items-center justify-between">
 <span v-if="updated_at" class="ds-pill ds-pill--sm">
 上次更新：{{ updated_at.slice(0, 19).replace('T', ' ') }}
 </span>
 <button type="button" class="ds-btn ds-btn--primary ml-auto" :disabled="saving || hasError" @click="save">{{ saving ? '保存中…' : '保存' }}</button>
 </div>
 </section>
 </main>
</template>
