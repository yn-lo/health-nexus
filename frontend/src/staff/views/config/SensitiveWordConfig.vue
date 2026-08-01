<script setup lang="ts">
/**
 * 敏感词库 — CRUD（分类 + 分页）
 * API: configApi.listSensitiveWords/createSensitiveWord/updateSensitiveWord/deleteSensitiveWord
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Pencil, Trash2, Search, AlertTriangle } from '@lucide/vue'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { AppHeader, DsPopup, StatRow } from '@/shared/components'
import { configApi } from '@/shared'
import { errmsg } from '@/shared/api/client'
import type { SensitiveWord, SensitiveWordCategory, SensitiveWordCreateRequest, SensitiveWordUpdateRequest } from '@/shared'

const router = useRouter()
const { showSuccessToast, showFailToast } = useDsToast()
const { showConfirmDialog } = useDsDialog()

const words = ref<SensitiveWord[]>([])
const loading = ref(false)
const search = ref('')
const filterCategory = ref<SensitiveWordCategory | 'all'>('all')
const page = ref(1)
const pageSize = 50
const total = ref(0)

const categoryOptions: { value: SensitiveWordCategory | 'all'; label: string }[] = [
 { value: 'all', label: '全部' },
 { value: 'suicide', label: '自杀' },
 { value: 'emergency', label: '急诊' },
 { value: 'injection', label: '注射' },
]

const formCategoryOptions: { value: SensitiveWordCategory; label: string }[] = [
 { value: 'suicide', label: '自杀' },
 { value: 'emergency', label: '急诊' },
 { value: 'injection', label: '注射' },
]

const categoryLabel: Record<SensitiveWordCategory, string> = {
 suicide: '自杀',
 emergency: '急诊',
 injection: '注射',
}

const categoryCounts = computed<Record<string, number>>(() => {
 const counts: Record<string, number> = { all: words.value.length }
 for (const c of ['suicide', 'emergency', 'injection'] as SensitiveWordCategory[]) {
 counts[c] = words.value.filter(w => w.category === c).length
 }
 return counts
})

const heroStats = computed(() => [
 { value: words.value.length, label: '总计' },
 { value: words.value.filter(w => w.is_active).length, label: '启用' },
 { value: words.value.filter(w => !w.is_active).length, label: '停用' },
])

const filtered = computed(() => {
 let list = words.value
 if (search.value.trim()) {
 const q = search.value.trim().toLowerCase()
 list = list.filter((w) => w.word.toLowerCase().includes(q))
 }
 return list
})

async function load() {
 loading.value = true
 try {
 const params: { page: number; page_size: number; category?: SensitiveWordCategory } = {
 page: page.value,
 page_size: pageSize,
 }
 if (filterCategory.value !== 'all') params.category = filterCategory.value
 const res = await configApi.listSensitiveWords(params)
 words.value = res.items
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
const editing = ref<SensitiveWord | null>(null)
const form = ref<SensitiveWordCreateRequest>(defaultForm())

function defaultForm(): SensitiveWordCreateRequest {
 return { word: '', category: 'suicide', is_active: true }
}

function openCreate() {
 editing.value = null
 form.value = defaultForm()
 showEditor.value = true
}

function openEdit(w: SensitiveWord) {
 editing.value = w
 form.value = { word: w.word, category: w.category, is_active: w.is_active }
 showEditor.value = true
}

async function submit() {
 if (!form.value.word.trim()) {
 showFailToast('请输入敏感词')
 return
 }
 try {
 if (editing.value) {
 const patch: SensitiveWordUpdateRequest = {
 word: form.value.word,
 category: form.value.category,
 is_active: form.value.is_active,
 }
 await configApi.updateSensitiveWord(editing.value.id, patch)
 showSuccessToast('已更新')
 } else {
 await configApi.createSensitiveWord(form.value)
 showSuccessToast('已创建')
 }
 showEditor.value = false
 await load()
 } catch (e) {
 showFailToast(errmsg(e, '保存失败'))
 }
}

async function toggleActive(w: SensitiveWord) {
 try {
 await configApi.updateSensitiveWord(w.id, { is_active: !w.is_active })
 w.is_active = !w.is_active
 } catch (e) {
 showFailToast(errmsg(e, '切换失败'))
 }
}

async function remove(w: SensitiveWord) {
 try {
 await showConfirmDialog({
 title: '确认删除',
 message: `删除敏感词「${w.word}」？`,
 confirmButtonText: '删除',
 danger: true,
 cancelButtonText: '取消',
 })
 } catch {
 return
 }
 try {
 await configApi.deleteSensitiveWord(w.id)
 words.value = words.value.filter((x) => x.id !== w.id)
 showSuccessToast('已删除')
 } catch (e) {
 showFailToast(errmsg(e, '删除失败'))
 }
}

onMounted(load)
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="敏感词库" @back="router.back">
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
 <div class="ds-search-box">
 <Search class="h-4 w-4 shrink-0 text-icon-brand" />
 <input v-model="search" type="text" placeholder="搜索敏感词" class="min-w-0 flex-1 border-none bg-transparent font-heading text-body-base text-text outline-none placeholder:text-text-tertiary">
 </div>
 <div class="flex gap-[var(--spacer-24)] border-b border-[var(--border-neutral-l1)] mt-[var(--spacer-12)] no-scrollbar overflow-x-auto">
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
 v-for="w in filtered"
 :key="w.id"
 class="ds-list-item ds-list-item--divider"
 >
 <span class="ds-list-item__icon ds-list-item__icon--brand">
 <AlertTriangle :size="20" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">{{ w.word }}</span>
 <span class="ds-list-item__meta">
 <span class="ds-tag ds-tag--default ds-tag--plain">{{ categoryLabel[w.category] }}</span>
 <span>· {{ w.is_active ? '启用' : '停用' }}</span>
 </span>
 </div>
 <div class="ds-list-item__trailing">
 <label class="ds-switch">
 <input type="checkbox" class="ds-switch__input" v-model="w.is_active" @change="toggleActive(w)">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 <button
 type="button"
 class="ds-list-item__action-btn"
 aria-label="编辑"
 @click="openEdit(w)"
 >
 <Pencil :size="16" />
 </button>
 <button
 type="button"
 class="ds-list-item__action-btn"
 aria-label="删除"
 @click="remove(w)"
 >
 <Trash2 :size="16" />
 </button>
 </div>
 </article>
 </div>
 <div v-else-if="!loading" class="flex flex-col items-center py-[var(--spacer-48)]">
 <div class="flex h-[56px] w-[56px] items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)]">
 <AlertTriangle class="h-6 w-6 text-icon-brand" />
 </div>
 <p class="mt-[var(--spacer-16)] text-heading-sm font-semibold text-text">还没有敏感词</p>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-tertiary">添加第一个敏感词</p>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--sm mt-[var(--spacer-16)]" @click="openCreate">新建</button>
 </div>
 </section>

 <DsPopup v-model:show="showEditor">
 <div class="p-[var(--spacer-16)] pb-[var(--spacer-24)]">
 <h3 class="mb-[var(--spacer-16)] text-heading-sm font-semibold text-text">
 {{ editing ? '编辑敏感词' : '新建敏感词' }}
 </h3>
 <div class="flex flex-col gap-[var(--spacer-12)]">
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">敏感词</span>
 <input v-model="form.word" class="ds-input" placeholder="如 自杀">
 </div>
 <div>
 <span class="mb-[var(--spacer-4)] block text-body-sm text-text-secondary">类别</span>
 <div class="flex gap-[var(--spacer-24)] border-b border-[var(--border-neutral-l1)]">
 <button
 v-for="opt in formCategoryOptions"
 :key="opt.value"
 type="button"
 :class="form.category === opt.value
 ? 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors font-medium text-text-brand'
 : 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors text-text-tertiary hover:text-text-brand'"
 @click="form.category = opt.value"
 >{{ opt.label }}<span v-if="form.category === opt.value" class="ds-tab-underline" /></button>
 </div>
 </div>
 <div class="flex items-center justify-between px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <span class="text-body-base text-text">启用</span>
 <label class="ds-switch">
 <input type="checkbox" class="ds-switch__input" v-model="form.is_active">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 </div>
 </div>
 <div class="mt-[var(--spacer-16)] flex gap-[var(--spacer-12)]">
 <button type="button" class="ds-btn ds-btn--secondary ds-btn--block" @click="showEditor = false">取消</button>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--block" @click="submit">保存</button>
 </div>
 </div>
 </DsPopup>
 </main>
</template>
