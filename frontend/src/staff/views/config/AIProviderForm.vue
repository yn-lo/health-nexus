<script setup lang="ts">
/**
 * AI 提供商编辑/新建表单
 * API: configApi.getAIProvider/createAIProvider/updateAIProvider/deleteAIProvider
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { Trash2, Eye, EyeOff } from '@lucide/vue'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { AppHeader } from '@/shared/components'
import { configApi, errmsg } from '@/shared'
import type { AIProviderType, AIProviderCreateRequest, AIProviderUpdateRequest } from '@/shared'

const router = useRouter()
const route = useRoute()
const { showSuccessToast, showFailToast } = useDsToast()
const { showConfirmDialog } = useDsDialog()

const loading = ref(false)
const saving = ref(false)
const showApiKey = ref(false)
const errors = ref<Record<string, string>>({})

const isEditMode = computed(() => !!route.params.id)
const pageTitle = computed(() => (isEditMode.value ? '编辑 AI 提供商' : '新建 AI 提供商'))

const typeOptions: { value: AIProviderType; label: string }[] = [
 { value: 'llm', label: 'LLM' },
 { value: 'embedding', label: 'Embedding' },
 { value: 'rerank', label: 'Rerank' },
 { value: 'rewrite', label: 'Rewrite' },
]

const form = ref<AIProviderCreateRequest>({
 provider_type: 'llm',
 name: '',
 api_base: '',
 api_key: '',
 model_name: '',
 dimension: undefined,
 params: {},
 is_active: true,
})

// params 编辑：LLM 类型拆为独立表单字段，其他类型保留 JSON 文本域
const paramsText = ref('{}')

/** LLM 常用参数（从 params 对象中读写） */
const llmTemperature = ref(0.7)
const llmTopP = ref(1)
const llmMaxTokens = ref<number | undefined>(undefined)

/** 从后端 params 对象同步到 LLM 表单字段 */
function syncParamsToForm(params: Record<string, unknown>) {
 llmTemperature.value = typeof params.temperature === 'number' ? params.temperature : 0.7
 llmTopP.value = typeof params.top_p === 'number' ? params.top_p : 1
 llmMaxTokens.value = typeof params.max_tokens === 'number' ? params.max_tokens : undefined
}

/** 从 LLM 表单字段组装回 params 对象（仅含用户修改过的字段） */
function buildLlmParams(): Record<string, unknown> {
 const p: Record<string, unknown> = {}
 if (llmTemperature.value !== 0.7) p.temperature = llmTemperature.value
 if (llmTopP.value !== 1) p.top_p = llmTopP.value
 if (llmMaxTokens.value !== undefined) p.max_tokens = llmMaxTokens.value
 return p
}

async function load() {
 if (!isEditMode.value) return
 loading.value = true
 try {
 const p = await configApi.getAIProvider(Number(route.params.id))
 form.value = {
 provider_type: p.provider_type,
 name: p.name,
 api_base: p.api_base,
 api_key: '',
 model_name: p.model_name,
 dimension: p.dimension ?? undefined,
 params: { ...p.params },
 is_active: p.is_active,
 }
 paramsText.value = JSON.stringify(p.params ?? {}, null, 2)
 if (p.provider_type === 'llm') syncParamsToForm(p.params ?? {})
 } catch (e) {
 showFailToast(errmsg(e, '加载失败'))
 router.back()
 } finally {
 loading.value = false
 }
}

async function submit() {
 errors.value = {}
 const e: Record<string, string> = {}
 if (!form.value.name.trim()) e.name = '请填写名称'
 if (!form.value.api_base.trim()) e.api_base = '请填写 API Base'
 if (!form.value.model_name.trim()) e.model_name = '请填写模型名'
 if (form.value.provider_type === 'embedding' && !form.value.dimension) e.dimension = 'Embedding 类型必须填写维度'
 if (!isEditMode.value && !form.value.api_key) e.api_key = '请填写 API Key'
 // 解析 params：LLM 用独立字段组装，其他类型解析 JSON 文本域
 let parsedParams: Record<string, unknown> = {}
 if (form.value.provider_type === 'llm') {
 parsedParams = buildLlmParams()
 } else {
 const trimmed = paramsText.value.trim()
 if (trimmed) {
 try {
 const parsed = JSON.parse(trimmed)
 if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
 e.params = '扩展参数必须是 JSON 对象'
 } else {
 parsedParams = parsed as Record<string, unknown>
 }
 } catch {
 e.params = '扩展参数 JSON 格式错误'
 }
 }
 }
 if (Object.keys(e).length > 0) {
 errors.value = e
 showFailToast('请补全必填项')
 return
 }
 form.value.params = parsedParams

 saving.value = true
 try {
 if (isEditMode.value) {
 const patch: AIProviderUpdateRequest = {
 name: form.value.name,
 api_base: form.value.api_base,
 model_name: form.value.model_name,
 dimension: form.value.dimension,
 params: form.value.params,
 is_active: form.value.is_active,
 }
 if (form.value.api_key) patch.api_key = form.value.api_key
 await configApi.updateAIProvider(Number(route.params.id), patch)
 showSuccessToast('已更新')
 } else {
 await configApi.createAIProvider(form.value)
 showSuccessToast('已创建')
 }
 router.push({ name: 'staff-config-ai-providers' })
 } catch (err) {
 showFailToast(errmsg(err, '保存失败'))
 } finally {
 saving.value = false
 }
}

async function remove() {
 try {
 await showConfirmDialog({
 title: '确认删除',
 message: `删除「${form.value.name}」？此操作不可恢复。`,
 confirmButtonText: '删除',
 danger: true,
 cancelButtonText: '取消',
 })
 } catch {
 return
 }
 try {
 await configApi.deleteAIProvider(Number(route.params.id))
 showSuccessToast('已删除')
 router.push({ name: 'staff-config-ai-providers' })
 } catch (e) {
 showFailToast(errmsg(e, '删除失败'))
 }
}

onMounted(() => {
 load()
})
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader :title="pageTitle" @back="router.back" />

 <!-- 表单字段 -->
 <section class="flex flex-col gap-[var(--spacer-20)] px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <!-- 基本信息 -->
 <div>
 <h3 class="text-body-sm font-medium text-text-secondary mb-[var(--spacer-8)] pl-[var(--spacer-12)] border-l-2 border-[var(--border-brand)]">基本信息</h3>
 <div class="flex flex-col gap-[var(--spacer-20)]">
 <!-- 类型 -->
 <div>
 <span class="mb-[var(--spacer-4)] block text-body-sm text-text-secondary">类型</span>
 <div class="flex gap-[var(--spacer-24)] border-b border-[var(--border-neutral-l1)]">
 <button
 v-for="opt in typeOptions"
 :key="opt.value"
 type="button"
 class="relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors"
 :class="[
 form.provider_type === opt.value ? 'font-medium text-text-brand' : 'text-text-tertiary hover:text-text-brand',
 { 'pointer-events-none opacity-60': isEditMode }
 ]"
 :disabled="isEditMode"
 @click="form.provider_type = opt.value"
 >{{ opt.label }}<span v-if="form.provider_type === opt.value" class="ds-tab-underline" /></button>
 </div>
 </div>

 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">名称</span>
 <input v-model="form.name" class="ds-input" :class="{ 'ds-input--error': errors.name }" placeholder="如 OpenAI GPT-4">
 <p v-if="errors.name" class="ds-field-error">{{ errors.name }}</p>
 </div>
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">API Base</span>
 <input v-model="form.api_base" class="ds-input" :class="{ 'ds-input--error': errors.api_base }" placeholder="https://api.openai.com/v1">
 <p v-if="errors.api_base" class="ds-field-error">{{ errors.api_base }}</p>
 </div>
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">API Key</span>
 <div class="relative">
 <input
 v-model="form.api_key"
 :type="showApiKey ? 'text' : 'password'"
 class="ds-input pr-10"
 :class="{ 'ds-input--error': errors.api_key }"
 :placeholder="isEditMode ? '留空不改' : 'sk-...'"
 >
 <button
 type="button"
 class="absolute right-3 top-1/2 -translate-y-1/2 text-text-tertiary hover:text-text-secondary"
 @click="showApiKey = !showApiKey"
 >
 <Eye v-if="showApiKey" :size="16" />
 <EyeOff v-else :size="16" />
 </button>
 </div>
 <p v-if="errors.api_key" class="ds-field-error">{{ errors.api_key }}</p>
 </div>
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">模型名</span>
 <input v-model="form.model_name" class="ds-input" :class="{ 'ds-input--error': errors.model_name }" placeholder="如 gpt-4">
 <p v-if="errors.model_name" class="ds-field-error">{{ errors.model_name }}</p>
 </div>
 <div v-if="form.provider_type === 'embedding'" class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">
 向量维度 <span class="text-[var(--status-error-default)]">*</span>
 </span>
 <input v-model.number="form.dimension" type="number" min="1" class="ds-input" :class="{ 'ds-input--error': errors.dimension }" placeholder="如 1024">
 <p v-if="errors.dimension" class="ds-field-error">{{ errors.dimension }}</p>
 <p v-else class="text-body-xs text-text-tertiary">embedding 类型必填，修改维度需先清空已有向量化切片</p>
 </div>
 </div>
 </div>

 <!-- 高级配置 -->
 <div>
 <h3 class="text-body-sm font-medium text-text-secondary mb-[var(--spacer-8)] pl-[var(--spacer-12)] border-l-2 border-[var(--border-brand)]">高级配置</h3>
 <div class="flex flex-col gap-[var(--spacer-20)]">
 <!-- LLM 类型：独立参数字段 -->
 <template v-if="form.provider_type === 'llm'">
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">Temperature</span>
 <input v-model.number="llmTemperature" type="number" min="0" max="2" step="0.1" class="ds-input" placeholder="0.7">
 <p class="text-body-xs text-text-tertiary">0~2，越高越随机，默认 0.7</p>
 </div>
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">Top P</span>
 <input v-model.number="llmTopP" type="number" min="0" max="1" step="0.1" class="ds-input" placeholder="1">
 <p class="text-body-xs text-text-tertiary">0~1，核采样概率，默认 1</p>
 </div>
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">Max Tokens</span>
 <input v-model.number="llmMaxTokens" type="number" min="1" class="ds-input" placeholder="不限">
 <p class="text-body-xs text-text-tertiary">单次回复最大 token 数，留空不限</p>
 </div>
 </template>
 <!-- 非 LLM 类型：JSON 文本域 -->
 <template v-else>
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">扩展参数 (JSON)</span>
 <textarea
 v-model="paramsText"
 rows="4"
 placeholder='{"dimension": 1024}'
 class="ds-textarea ds-textarea--mono"
 :class="{ 'ds-textarea--error': errors.params }"
 />
 <p v-if="errors.params" class="ds-field-error">{{ errors.params }}</p>
 <p v-else class="text-body-xs text-text-tertiary">可选，供应商扩展参数</p>
 </div>
 </template>
 <div class="flex items-center justify-between px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <span class="text-body-base text-text">启用</span>
 <label class="ds-switch">
 <input type="checkbox" class="ds-switch__input" v-model="form.is_active">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 </div>
 </div>
 </div>
 </section>

 <!-- 底部操作栏 -->
 <div class="fixed inset-x-0 bottom-0 z-40 border-t border-[var(--border-neutral-l1)] bg-[var(--bg-base-default)]">
 <div class="mx-auto flex max-w-[480px] gap-[var(--spacer-12)] px-[var(--spacer-16)] py-[var(--spacer-12)]">
 <button
 v-if="isEditMode"
 type="button"
 class="ds-btn ds-btn--danger-outline"
 :disabled="saving"
 @click="remove"
 >
 <Trash2 :size="14" />
 删除
 </button>
 <button type="button" class="ds-btn ds-btn--primary" :class="isEditMode ? 'flex-1' : 'ds-btn--block'" :disabled="saving" @click="submit">{{ saving ? '保存中…' : '保存' }}</button>
 </div>
 </div>
 </main>
</template>
