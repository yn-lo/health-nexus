<script setup lang="ts">
/**
 * 安全规则 — CRUD（类别 + 动作）
 * API: configApi.listSafetyRules/createSafetyRule/updateSafetyRule/deleteSafetyRule
 * ⚠️ 预配置：当前阶段未被聊天流消费，为阶段 2 可配置扩展预留
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Pencil, Trash2, Search, ShieldBan } from '@lucide/vue'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { AppHeader, DsPopup, StatRow } from '@/shared/components'
import { configApi } from '@/shared'
import { errmsg } from '@/shared/api/client'
import type { SafetyRule, SafetyRuleCategory, SafetyRuleAction, SafetyRuleCreateRequest, SafetyRuleUpdateRequest } from '@/shared'

const router = useRouter()
const { showSuccessToast, showFailToast } = useDsToast()
const { showConfirmDialog } = useDsDialog()

const rules = ref<SafetyRule[]>([])
const loading = ref(false)
const search = ref('')
const filterCategory = ref<SafetyRuleCategory | 'all'>('all')
const page = ref(1)
const pageSize = 50
const total = ref(0)

const categoryOptions: { value: SafetyRuleCategory | 'all'; label: string }[] = [
 { value: 'all', label: '全部' },
 { value: 'diagnosis', label: '诊断越权' },
 { value: 'prescription', label: '处方' },
 { value: 'stop_medication', label: '停药' },
 { value: 'delay_medical', label: '延误就医' },
 { value: 'other', label: '其他' },
]

const categoryLabel: Record<SafetyRuleCategory, string> = {
 diagnosis: '诊断越权',
 prescription: '处方',
 stop_medication: '停药',
 delay_medical: '延误就医',
 other: '其他',
}

const actionLabel: Record<SafetyRuleAction, string> = {
 replace: '替换',
 block: '拦截',
}

const categoryCounts = computed<Record<string, number>>(() => {
 const counts: Record<string, number> = { all: rules.value.length }
 for (const c of ['diagnosis', 'prescription', 'stop_medication', 'delay_medical', 'other'] as SafetyRuleCategory[]) {
 counts[c] = rules.value.filter(r => r.category === c).length
 }
 return counts
})

const heroStats = computed(() => [
 { value: rules.value.length, label: '总计' },
 { value: rules.value.filter(r => r.is_active).length, label: '启用' },
 { value: rules.value.filter(r => !r.is_active).length, label: '停用' },
])

const filtered = computed(() => {
 let list = rules.value
 if (search.value.trim()) {
 const q = search.value.trim().toLowerCase()
 list = list.filter(
 (r) => r.name.toLowerCase().includes(q) || r.pattern.toLowerCase().includes(q),
 )
 }
 return list
})

async function load() {
 loading.value = true
 try {
 const params: { page: number; page_size: number; category?: SafetyRuleCategory } = {
 page: page.value,
 page_size: pageSize,
 }
 if (filterCategory.value !== 'all') params.category = filterCategory.value
 const res = await configApi.listSafetyRules(params)
 rules.value = res.items
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

const showEditor = ref(false)
const editing = ref<SafetyRule | null>(null)
const form = ref<SafetyRuleCreateRequest>(defaultForm())

function defaultForm(): SafetyRuleCreateRequest {
 return {
 name: '',
 category: 'diagnosis',
 pattern: '',
 action: 'replace',
 replacement: '',
 is_active: true,
 description: '',
 }
}

function openCreate() {
 editing.value = null
 form.value = defaultForm()
 showEditor.value = true
}

function openEdit(r: SafetyRule) {
 editing.value = r
 form.value = {
 name: r.name,
 category: r.category,
 pattern: r.pattern,
 action: r.action,
 replacement: r.replacement,
 is_active: r.is_active,
 description: r.description,
 }
 showEditor.value = true
}

async function submit() {
 if (!form.value.name.trim() || !form.value.pattern.trim()) {
 showFailToast('请补全名称和模式')
 return
 }
 if (form.value.action === 'replace' && !form.value.replacement?.trim()) {
 showFailToast('替换动作必须填写替换文本')
 return
 }
 try {
 if (editing.value) {
 const patch: SafetyRuleUpdateRequest = {
 name: form.value.name,
 category: form.value.category,
 pattern: form.value.pattern,
 action: form.value.action,
 replacement: form.value.replacement,
 is_active: form.value.is_active,
 description: form.value.description,
 }
 await configApi.updateSafetyRule(editing.value.id, patch)
 showSuccessToast('已更新')
 } else {
 await configApi.createSafetyRule(form.value)
 showSuccessToast('已创建')
 }
 showEditor.value = false
 await load()
 } catch (e) {
 showFailToast(errmsg(e, '保存失败'))
 }
}

async function toggleActive(r: SafetyRule) {
 try {
 await configApi.updateSafetyRule(r.id, { is_active: !r.is_active })
 r.is_active = !r.is_active
 } catch (e) {
 showFailToast(errmsg(e, '切换失败'))
 }
}

async function remove(r: SafetyRule) {
 try {
 await showConfirmDialog({
 title: '确认删除',
 message: `删除规则「${r.name}」？`,
 confirmButtonText: '删除',
 danger: true,
 cancelButtonText: '取消',
 })
 } catch {
 return
 }
 try {
 await configApi.deleteSafetyRule(r.id)
 rules.value = rules.value.filter((x) => x.id !== r.id)
 showSuccessToast('已删除')
 } catch (e) {
 showFailToast(errmsg(e, '删除失败'))
 }
}

onMounted(load)
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="安全规则" @back="router.back">
 <template #right>
 <button
 type="button"
 class="ds-icon-btn ds-icon-btn--sm ds-icon-btn--brand"
 aria-label="新增"
 @click="openCreate"
 >
 <Plus class="icon h-5 w-5" />
 </button>
 </template>
 </AppHeader>

 <section class="mx-[var(--spacer-16)] mt-[var(--spacer-12)] rounded-[var(--radius-card-large)] bg-[var(--ai-gradient-soft)] px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <StatRow :stats="heroStats" />
 </section>

 <section class="px-[var(--spacer-16)] pt-[var(--spacer-12)] pb-[var(--spacer-8)]">
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">
 ⚠️ 预配置：当前阶段规则存储在数据库但未被聊天流消费，为阶段 2 可配置扩展预留。
 </p>
 <p class="mb-[var(--spacer-12)] text-body-sm text-text-tertiary">
 安全规则（正则模式匹配 AI 输出内容）与
 <router-link :to="{ name: 'staff-config-safety-messages' }" class="text-text-brand underline">安全话术</router-link>
 （命中后向患者展示的文案）互相配合：规则决定「何时拦截/替换」，话术决定「展示什么内容」。当前系统的输出审查仍在用内置默认规则集。
 </p>
 <div class="ds-search-box">
 <Search class="h-4 w-4 shrink-0 text-icon-brand" />
 <input v-model="search" type="text" placeholder="搜索名称或正则" class="min-w-0 flex-1 border-none bg-transparent font-heading text-body-base text-text outline-none placeholder:text-text-tertiary">
 </div>
 <div class="flex gap-[var(--spacer-24)] border-b border-[var(--border-neutral-l1)] no-scrollbar overflow-x-auto mt-[var(--spacer-12)]">
 <button
 v-for="opt in categoryOptions"
 :key="opt.value"
 type="button"
 :class="filterCategory === opt.value
 ? 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors font-medium text-text-brand'
 : 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors text-text-tertiary hover:text-text-brand'"
 @click="filterCategory = opt.value; onFilterChange()"
 >{{ opt.label }}<span v-if="categoryCounts[opt.value] !== undefined && categoryCounts[opt.value] > 0" class="ml-[var(--spacer-4)] inline-flex items-center justify-center min-w-[16px] h-[16px] px-[var(--spacer-4)] rounded-[var(--radius-full)] text-[10px] font-medium leading-none transition-colors" :class="filterCategory === opt.value ? 'bg-[var(--bg-brand-light)] text-text-brand' : 'bg-[var(--bg-overlay-l1)] text-text-tertiary'">{{ categoryCounts[opt.value] }}</span><span v-if="filterCategory === opt.value" class="ds-tab-underline" /></button>
 </div>
 </section>

 <section class="px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <div v-if="filtered.length > 0" class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
 <article
 v-for="r in filtered"
 :key="r.id"
 class="ds-list-item ds-list-item--divider"
 >
 <span
 class="ds-list-item__icon"
 :class="r.action === 'block' ? 'ds-list-item__icon--error' : 'ds-list-item__icon--alert'"
 >
 <ShieldBan :size="20" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">{{ r.name }}</span>
 <span class="ds-list-item__meta">
 <span>{{ categoryLabel[r.category] }}</span>
 <span>· {{ actionLabel[r.action] }}</span>
 <span>· <code class="font-mono">{{ r.pattern }}</code></span>
 <span v-if="r.action === 'replace'">· 替换为 {{ r.replacement }}</span>
 </span>
 </div>
 <div class="ds-list-item__trailing">
 <span class="ds-tag ds-tag--plain" :class="r.is_active ? 'ds-tag--success' : 'ds-tag--default'">{{ r.is_active ? '启用' : '停用' }}</span>
 <button type="button" class="ds-btn ds-btn--secondary ds-btn--sm" @click="toggleActive(r)">{{ r.is_active ? '停用' : '启用' }}</button>
 <button
 type="button"
 class="ds-list-item__action-btn"
 aria-label="编辑"
 @click="openEdit(r)"
 >
 <Pencil :size="16" />
 </button>
 <button
 type="button"
 class="ds-list-item__action-btn"
 aria-label="删除"
 @click="remove(r)"
 >
 <Trash2 :size="16" />
 </button>
 </div>
 </article>
 </div>
 <div v-else-if="!loading" class="flex flex-col items-center py-[var(--spacer-48)]">
 <div class="flex h-[56px] w-[56px] items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)]">
 <ShieldBan class="h-6 w-6 text-icon-brand" />
 </div>
 <p class="mt-[var(--spacer-16)] text-heading-sm font-semibold text-text">还没有安全规则</p>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-tertiary">添加第一条安全规则</p>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--sm mt-[var(--spacer-16)]" @click="openCreate">新建</button>
 </div>
 </section>

 <DsPopup v-model:show="showEditor">
 <div class="max-h-[85vh] overflow-y-auto p-[var(--spacer-16)] pb-[var(--spacer-24)]">
 <h3 class="mb-[var(--spacer-16)] text-heading-sm font-semibold text-text">
 {{ editing ? '编辑安全规则' : '新建安全规则' }}
 </h3>
 <div class="flex flex-col gap-[var(--spacer-12)]">
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">名称</span>
 <input v-model="form.name" class="ds-input" placeholder="如 诊断越权检测">
 </div>
 <div>
 <span class="mb-[var(--spacer-4)] block text-body-sm text-text-secondary">类别</span>
 <div class="flex gap-[var(--spacer-24)] border-b border-[var(--border-neutral-l1)]">
 <button
 v-for="opt in categoryOptions.filter(o => o.value !== 'all')"
 :key="opt.value"
 type="button"
 :class="form.category === opt.value
 ? 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors font-medium text-text-brand'
 : 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors text-text-tertiary hover:text-text-brand'"
 @click="form.category = opt.value as SafetyRuleCategory"
 >{{ opt.label }}<span v-if="form.category === opt.value" class="ds-tab-underline" /></button>
 </div>
 </div>
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">正则模式</span>
 <input v-model="form.pattern" class="ds-input ds-input--mono" placeholder="如 确诊|诊断为">
 </div>
 <div>
 <span class="mb-[var(--spacer-4)] block text-body-sm text-text-secondary">动作</span>
 <div class="flex gap-[var(--spacer-24)] border-b border-[var(--border-neutral-l1)]">
 <button
 type="button"
 :class="form.action === 'replace'
 ? 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors font-medium text-text-brand'
 : 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors text-text-tertiary hover:text-text-brand'"
 @click="form.action = 'replace'"
 >替换<span v-if="form.action === 'replace'" class="ds-tab-underline" /></button>
 <button
 type="button"
 :class="form.action === 'block'
 ? 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors font-medium text-text-brand'
 : 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors text-text-tertiary hover:text-text-brand'"
 @click="form.action = 'block'"
 >拦截<span v-if="form.action === 'block'" class="ds-tab-underline" /></button>
 </div>
 </div>
 <div v-if="form.action === 'replace'" class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">替换文本</span>
 <textarea v-model="form.replacement" class="ds-textarea" rows="2" placeholder="如 具体诊断请咨询您的主治医生"></textarea>
 </div>
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">描述（可选）</span>
 <textarea v-model="form.description" class="ds-textarea" rows="2" placeholder="规则说明"></textarea>
 </div>
 <div class="flex items-center justify-between px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <span class="text-body-base text-text">启用</span>
 <label class="ds-switch">
 <input type="checkbox" class="ds-switch__input" v-model="form.is_active">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 </div>
 </div>
 <div class="mt-[var(--spacer-16)] flex flex-col gap-[var(--spacer-12)]">
 <button type="button" class="ds-btn ds-btn--secondary ds-btn--block" @click="showEditor = false">取消</button>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--block" @click="submit">保存</button>
 </div>
 </div>
</DsPopup>
 </main>
</template>


