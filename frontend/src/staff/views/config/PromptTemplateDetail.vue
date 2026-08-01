<script setup lang="ts">
/**
 * Prompt 模板详情 — 新建 / 预览 / 同页编辑（三态复用）
 * 路由：profile/config/prompts/new → 新建（无 id，直接编辑态）
 * profile/config/prompts/:id → 查看/编辑（有 id，先加载再预览）
 * API: configApi.listPromptTemplates（后端无单条 GET，按 id 客户端过滤）
 * configApi.createPromptTemplate/updatePromptTemplate/deletePromptTemplate
 * 行为：is_active=true 时同 type+department 下其他自动失活；生效版本不可删除（409）
 * 激活唯一入口：预览态「设为生效版本」按钮（带确认弹窗）；编辑内容不改变生效状态
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { FileText, Pencil, Trash2, CheckCircle2, Building2, History, Clock, ChevronDown, TriangleAlert } from '@lucide/vue'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { AppHeader } from '@/shared/components'
import { configApi, errmsg, useDepartmentOptions, DEPARTMENT_GLOBAL, fmtDateTime } from '@/shared'
import type { PromptTemplate, PromptTemplateCreateRequest } from '@/shared'

const router = useRouter()
const route = useRoute()
const { showSuccessToast, showFailToast } = useDsToast()
const { showConfirmDialog } = useDsDialog()

const isCreate = computed(() => route.name === 'staff-config-prompt-create')
const templateId = computed(() => Number(route.params.id))

const template = ref<PromptTemplate | null>(null)
const loading = ref(false)
const mode = ref<'preview' | 'edit'>(isCreate.value ? 'edit' : 'preview')
const saving = ref(false)
const justActivated = ref(false)

const typeLabel: Record<string, string> = {
 system: 'System',
}

function typeDisplay(t: { type: string }): string {
 return typeLabel[t.type] ?? t.type
}

const { options: departmentOptions, load: loadDepartments } = useDepartmentOptions(DEPARTMENT_GLOBAL)

const departmentLabel = computed(() => {
 if (!template.value?.department_id) return '全局默认'
 const hit = departmentOptions.value.find((d) => d.id === template.value?.department_id)
 return hit?.label ?? `科室 #${template.value.department_id}`
})

const form = ref<PromptTemplateCreateRequest>({
 type: 'system',
 content: '',
 is_active: false,
 description: '',
 department_id: null,
})

async function load() {
 if (isCreate.value) return
 loading.value = true
 try {
 const res = await configApi.listPromptTemplates({ page: 1, page_size: 100 })
 template.value = res.items.find((t) => t.id === templateId.value) ?? null
 } catch (e) {
 showFailToast(errmsg(e, '加载失败'))
 } finally {
 loading.value = false
 }
}

function startEdit() {
 if (!template.value) return
 justActivated.value = false
 form.value = {
 type: template.value.type,
 content: template.value.content,
 is_active: template.value.is_active,
 description: template.value.description,
 department_id: template.value.department_id,
 }
 mode.value = 'edit'
}

function cancelEdit() {
 if (isCreate.value) {
 router.replace({ name: 'staff-config-prompts' })
 return
 }
 mode.value = 'preview'
}

async function save() {
 if (!form.value.content.trim()) {
 showFailToast('模板内容不能为空')
 return
 }
 saving.value = true
 try {
 if (isCreate.value) {
 const created = await configApi.createPromptTemplate(form.value)
 showSuccessToast('已创建')
 router.replace({ name: 'staff-config-prompt-detail', params: { id: created.id } })
 } else if (template.value) {
 // 编辑仅更新内容；生效状态由预览态「设为生效版本」单独管理，避免随手保存静默切换激活
 template.value = await configApi.updatePromptTemplate(template.value.id, {
 content: form.value.content,
 })
 mode.value = 'preview'
 showSuccessToast('已保存')
 }
 } catch (e) {
 showFailToast(errmsg(e, '保存失败'))
 } finally {
 saving.value = false
 }
}

async function activate() {
 if (!template.value) return
 try {
 await showConfirmDialog({
 title: '设为生效版本',
 message: `将此 ${typeDisplay(template.value)} 模板（v${template.value.version}）设为生效？同类型其他模板将自动失活。`,
 confirmButtonText: '设为生效',
 cancelButtonText: '取消',
 })
 } catch {
 return
 }
 try {
 template.value = await configApi.updatePromptTemplate(template.value.id, { is_active: true })
 justActivated.value = true
 showSuccessToast('已生效')
 } catch (e) {
 showFailToast(errmsg(e, '激活失败'))
 }
}

async function remove() {
 if (!template.value) return
 if (template.value.is_active) {
 showFailToast('生效版本不可删除，请先激活其他版本')
 return
 }
 try {
 await showConfirmDialog({
 title: '确认删除',
 message: `删除此模板（v${template.value.version}）？`,
 confirmButtonText: '删除',
 danger: true,
 cancelButtonText: '取消',
 })
 } catch {
 return
 }
 try {
 await configApi.deletePromptTemplate(template.value.id)
 showSuccessToast('已删除')
 router.replace({ name: 'staff-config-prompts' })
 } catch (e) {
 showFailToast(errmsg(e, '删除失败'))
 }
}

onMounted(() => {
 load()
 loadDepartments()
})
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader :title="isCreate ? '新建模板' : '模板详情'" @back="router.back" />

 <div v-if="loading" class="flex items-center justify-center py-20">
 <div class="ds-loading">
 <span class="ds-loading__spinner" />
 <span class="ds-loading__text">加载中</span>
 </div>
 </div>

 <template v-else-if="isCreate || template">
 <section v-if="!isCreate" class="px-[var(--spacer-16)] pt-[var(--spacer-16)]">
 <div class="flex items-center gap-[var(--spacer-16)]">
 <span
 class="flex h-14 w-14 shrink-0 items-center justify-center rounded-[var(--radius-full)] transition-colors"
 :class="template!.is_active ? 'bg-[var(--status-success-surface-l1)]' : 'bg-[var(--ai-gradient-soft)]'"
 >
 <FileText :size="28" :class="template!.is_active ? 'text-[var(--status-success-default)]' : 'text-icon-brand'" />
 </span>
 <div class="min-w-0">
 <h1 class="text-heading-sm font-semibold text-text">
 {{ typeDisplay(template!) }}模板 · v{{ template!.version }}
 </h1>
 <div class="mt-[var(--spacer-4)] flex flex-wrap items-center gap-[var(--spacer-8)]">
 <span v-if="template!.is_active" class="ds-tag ds-tag--success ds-tag--plain">
 <CheckCircle2 :size="12" />
 生效中
 </span>
 <span v-else class="ds-tag ds-tag--default ds-tag--plain">未生效</span>
 <span class="ds-tag ds-tag--primary ds-tag--plain">{{ departmentLabel }}</span>
 </div>
 </div>
 </div>
 <p v-if="template!.description" class="mt-[var(--spacer-12)] text-body-sm text-text-secondary">
 {{ template!.description }}
 </p>
 </section>

 <section class="px-[var(--spacer-16)]" :class="isCreate ? 'pt-[var(--spacer-16)]' : 'pt-[var(--spacer-24)]'">
 <div class="ds-card overflow-hidden">
 <div v-if="!isCreate" class="flex items-center justify-between border-b border-[var(--border-neutral-l1)] px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <span class="font-heading text-body-base font-medium text-text">模板内容</span>
 <button v-if="mode === 'preview'" type="button" class="ds-btn ds-btn--ghost ds-btn--sm" @click="startEdit">
 <Pencil :size="14" />
 编辑
 </button>
 </div>

 <div v-if="!isCreate && mode === 'preview'" class="p-[var(--spacer-16)]">
 <div class="max-h-[45vh] overflow-y-auto whitespace-pre-wrap break-words rounded-[var(--radius-8)] bg-[var(--bg-overlay-l1)] p-[var(--spacer-12)] text-body-base text-text">{{ template!.content }}</div>
 </div>

 <div v-else class="px-[var(--spacer-16)] py-[var(--spacer-12)]">
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">模板内容</span>
 <textarea v-model="form.content" class="ds-textarea" rows="10" placeholder="你是一个健康宣教助手..."></textarea>
 </div>
 <div v-if="isCreate" class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">描述</span>
 <input v-model="form.description" class="ds-input" placeholder="模板说明">
 </div>
 <div v-if="isCreate" class="mt-[var(--spacer-12)] flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">所属科室</span>
 <div class="relative">
 <select
 v-model="form.department_id"
 class="ds-select"
 >
 <option v-for="d in departmentOptions" :key="d.id ?? 'global'" :value="d.id">
 {{ d.label }}
 </option>
 </select>
 <ChevronDown class="pointer-events-none absolute right-[var(--spacer-12)] top-1/2 h-4 w-4 -translate-y-1/2 text-icon-tertiary" />
 </div>
 <p class="text-body-xs text-text-tertiary">为指定科室创建专属模板；留空表示全局默认</p>
 </div>
 <div v-if="isCreate" class="mt-[var(--spacer-12)] flex items-center justify-between border-t border-[var(--border-neutral-l1)] pt-[var(--spacer-12)]">
 <span class="text-body-sm text-text-secondary">创建后立即生效（同类型其他模板将自动失活）</span>
 <label class="ds-switch">
 <input type="checkbox" class="ds-switch__input" v-model="form.is_active">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 </div>
 <div class="mt-[var(--spacer-16)] flex gap-[var(--spacer-12)]">
 <button type="button" class="ds-btn ds-btn--secondary ds-btn--block" :disabled="saving" @click="cancelEdit">取消</button>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--block" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存' }}</button>
 </div>
 </div>
 </div>
 </section>

 <template v-if="!isCreate && template">
 <section class="px-[var(--spacer-16)] pt-[var(--spacer-12)]">
 <h2 class="text-body-sm font-medium text-text-secondary mb-[var(--spacer-8)] pl-[var(--spacer-12)] border-l-2 border-[var(--border-brand)]">模板信息</h2>
 <div class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
 <div class="ds-list-item ds-list-item--divider">
 <span class="ds-list-item__icon">
 <Building2 :size="20" class="text-icon-secondary" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">所属科室</span>
 <span class="ds-list-item__meta">{{ departmentLabel }}</span>
 </div>
 </div>
 <div class="ds-list-item ds-list-item--divider">
 <span class="ds-list-item__icon">
 <History :size="20" class="text-icon-secondary" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">创建时间</span>
 <span class="ds-list-item__meta">{{ fmtDateTime(template.created_at) }}</span>
 </div>
 </div>
 <div class="ds-list-item">
 <span class="ds-list-item__icon">
 <Clock :size="20" class="text-icon-secondary" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">最近更新</span>
 <span class="ds-list-item__meta">{{ fmtDateTime(template.updated_at) }}</span>
 </div>
 </div>
 </div>
 </section>

 <section class="flex flex-col gap-[var(--spacer-12)] px-[var(--spacer-16)] pt-[var(--spacer-24)]">
 <div v-if="!template.is_active" class="ds-alert ds-alert--warning" role="note">
 <TriangleAlert class="icon" />
 <span>此版本未生效，AI 对话不会使用它；设为生效后，同类型其他版本将自动失活</span>
 </div>
 <div v-else-if="justActivated" class="ds-alert ds-alert--success" role="status">
 <CheckCircle2 class="icon" />
 <span>已设为生效版本，同类型其他版本已自动失活</span>
 </div>
 <button v-if="!template.is_active" type="button" class="ds-btn ds-btn--primary ds-btn--block" @click="activate"><CheckCircle2 :size="16" /> 设为生效版本</button>
 <button type="button" class="ds-btn ds-btn--danger-outline ds-btn--block" :disabled="template.is_active" @click="remove"><Trash2 :size="16" /> 删除模板</button>
 <p v-if="template.is_active && !justActivated" class="text-center text-body-xs text-text-tertiary">
 生效版本不可删除，请先激活其他版本
 </p>
 </section>
 </template>
 </template>

 <div v-else class="flex flex-col items-center py-20">
 <span class="flex h-14 w-14 items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)]">
 <FileText :size="28" class="text-icon-brand" />
 </span>
 <p class="mt-[var(--spacer-12)] text-heading-sm font-semibold text-text">模板不存在</p>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-tertiary">该模板可能已被删除</p>
 </div>
 </main>
</template>
