<script setup lang="ts">
/**
 * 账号管理 — 管理员账户列表 + 创建 + 锁定/解锁
 * API: authApi.listStaffAccounts/createStaffAccount/lockStaffAccount/unlockStaffAccount
 * 后端禁止锁定自己（409 AUTH_SELF_LOCK），无编辑/删除端点。
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { UserCog, Eye, EyeOff } from '@lucide/vue'
import { useDsToast } from '@/shared/composables'
import { AppHeader, DsPopup, StatRow, DsFilterTabs, DsSearchBox } from '@/shared/components'
import { authApi, errmsg, usePagedList, getUserStored } from '@/shared'
import { ROLE_LABEL, STAFF_ROLES, PATIENT_ROLES, type UserRole } from '@/shared/constants/roles'
import type { StaffAccount, StaffAccountCreateRequest } from '@/shared'

const router = useRouter()
const { showSuccessToast, showFailToast } = useDsToast()
const currentUserId = getUserStored()?.id ?? 0

// ponytail: 后端支持分页，但单页 50 足以覆盖典型部署的账户规模；超量时再加翻页 UI
const { items: accounts, loading, load } = usePagedList<StaffAccount>({
 pageSize: 50,
 fetcher: (params) => authApi.listStaffAccounts(params),
})
const search = ref('')
const filterRole = ref<UserRole | 'all'>('all')

/** 全部角色（STAFF + PATIENT），顺序：管理员 → 医护 → 患者 */
const ALL_ROLES: UserRole[] = [...STAFF_ROLES, ...PATIENT_ROLES]

const roleOptions: { value: UserRole | 'all'; label: string }[] = [
 { value: 'all', label: '全部' },
 ...ALL_ROLES.map((r) => ({ value: r, label: ROLE_LABEL[r] })),
]

const formRoleOptions: { value: UserRole; label: string }[] = ALL_ROLES.map((r) => ({
 value: r,
 label: ROLE_LABEL[r],
}))

const filtered = computed(() => {
 let list = accounts.value
 if (search.value.trim()) {
 const q = search.value.trim().toLowerCase()
 list = list.filter((a) => a.username.toLowerCase().includes(q))
 }
 if (filterRole.value !== 'all') {
 list = list.filter((a) => a.role === filterRole.value)
 }
 return list
})

const totalCount = computed(() => accounts.value.length)
const activeCount = computed(() => accounts.value.filter(a => a.is_active).length)
const lockedCount = computed(() => accounts.value.filter(a => !a.is_active).length)

const heroStats = computed(() => [
 { value: totalCount.value, label: '总计' },
 { value: activeCount.value, label: '正常' },
 { value: lockedCount.value, label: '已锁定' },
])

const roleCounts = computed<Record<string, number>>(() => {
 const counts: Record<string, number> = { all: accounts.value.length }
 for (const r of ALL_ROLES) {
 counts[r] = accounts.value.filter(a => a.role === r).length
 }
 return counts
})

const showEditor = ref(false)
const showPassword = ref(false)
const form = ref<StaffAccountCreateRequest>(defaultForm())

function defaultForm(): StaffAccountCreateRequest {
 // 默认选最后一个医护角色（NURSE）— 通过常量索引避免硬编码字面量
 return { username: '', password: '', role: STAFF_ROLES[STAFF_ROLES.length - 1] }
}

function openCreate() {
 form.value = defaultForm()
 showEditor.value = true
}

async function submit() {
 const u = form.value.username.trim()
 if (!/^[\p{L}\p{N}_]{3,64}$/u.test(u)) {
 showFailToast('用户名需为 3-64 位字母、数字或下划线')
 return
 }
 if (form.value.password.length < 8 || !/\p{L}/u.test(form.value.password) || !/\p{N}/u.test(form.value.password)) {
 showFailToast('密码至少 8 位，需含字母和数字')
 return
 }
 try {
 await authApi.createStaffAccount({ ...form.value, username: u })
 showSuccessToast('已创建')
 showEditor.value = false
 await load()
 } catch (e) {
 showFailToast(errmsg(e, '创建失败'))
 }
}

async function toggleLock(a: StaffAccount) {
 try {
 if (a.is_active) {
 await authApi.lockStaffAccount(a.id)
 a.is_active = false
 showSuccessToast('已锁定')
 } else {
 await authApi.unlockStaffAccount(a.id)
 a.is_active = true
 showSuccessToast('已解锁')
 }
 } catch (e) {
 showFailToast(errmsg(e, '操作失败'))
 }
}

onMounted(load)
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="账号管理" showCreate @create="openCreate" @back="router.back" />

 <section class="mx-[var(--spacer-16)] mt-[var(--spacer-12)] rounded-[var(--radius-card-large)] bg-[var(--ai-gradient-soft)] px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <StatRow :stats="heroStats" />
 </section>

 <section class="px-[var(--spacer-16)] pt-[var(--spacer-12)] pb-[var(--spacer-8)]">
 <DsSearchBox v-model="search" placeholder="搜索用户名" />
 <DsFilterTabs v-model="filterRole" :options="roleOptions" :counts="roleCounts" />
 </section>

 <section class="px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <div v-if="filtered.length > 0" class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
 <article
 v-for="a in filtered"
 :key="a.id"
 class="ds-list-item ds-list-item--divider cursor-pointer"
 @click="router.push({ name: 'staff-config-account-detail', params: { id: a.id } })"
 >
 <span class="ds-list-item__icon ds-list-item__icon--brand">
 <UserCog :size="20" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">
 {{ a.username }}<span v-if="a.id === currentUserId" class="ml-[var(--spacer-4)] text-body-xs text-text-tertiary">（我）</span>
 </span>
 <span class="ds-list-item__meta">
 <span class="ds-tag ds-tag--primary ds-tag--plain">{{ ROLE_LABEL[a.role] }}</span>
 <span>· {{ a.is_active ? '正常' : '已锁定' }}</span>
 </span>
 </div>
 <div class="ds-list-item__trailing" @click.stop>
 <label class="ds-switch ds-switch--sm" :class="{ 'pointer-events-none opacity-50': a.id === currentUserId }">
 <input type="checkbox" class="ds-switch__input" :checked="a.is_active" :disabled="a.id === currentUserId" @change="toggleLock(a)">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 </div>
 </article>
 </div>
 <div v-else-if="!loading" class="flex flex-col items-center py-20">
 <span class="flex h-14 w-14 items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)]">
 <UserCog :size="28" class="text-icon-brand" />
 </span>
 <p class="mt-[var(--spacer-12)] text-body-base font-medium text-text">还没有账户</p>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-tertiary">创建第一个管理员账户</p>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--sm mt-[var(--spacer-16)]" @click="openCreate">新建</button>
 </div>
 </section>

 <DsPopup v-model:show="showEditor">
 <div class="p-[var(--spacer-16)] pb-[var(--spacer-24)]">
 <h3 class="mb-[var(--spacer-16)] text-heading-sm font-semibold text-text">
 新建账户
 </h3>
 <div class="flex flex-col gap-[var(--spacer-12)]">
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">用户名<span class="text-[var(--status-error-default)]">*</span></span>
 <input v-model="form.username" class="ds-input" placeholder="3-64 字符，字母/数字/下划线">
 </div>
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">密码<span class="text-[var(--status-error-default)]">*</span></span>
 <div class="ds-field-wrap">
 <input v-model="form.password" :type="showPassword ? 'text' : 'password'" placeholder="至少 8 位，含字母和数字">
 <button type="button" class="inline-flex h-6 w-6 shrink-0 items-center justify-center p-0 text-icon-tertiary hover:text-icon" :aria-label="showPassword ? '隐藏密码' : '显示密码'" @click="showPassword = !showPassword">
 <Eye v-if="showPassword" class="h-4 w-4" />
 <EyeOff v-else class="h-4 w-4" />
 </button>
 </div>
 </div>
 <div>
 <span class="mb-[var(--spacer-4)] block text-body-sm text-text-secondary">角色</span>
 <div class="flex gap-[var(--spacer-24)] border-b border-[var(--border-neutral-l1)] no-scrollbar overflow-x-auto">
 <button
 v-for="opt in formRoleOptions"
 :key="opt.value"
 type="button"
 :class="form.role === opt.value
 ? 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors font-medium text-text-brand'
 : 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors text-text-tertiary hover:text-text-brand'"
 @click="form.role = opt.value"
 >{{ opt.label }}<span v-if="form.role === opt.value" class="ds-tab-underline" /></button>
 </div>
 </div>
 </div>
 <div class="mt-[var(--spacer-16)] flex gap-[var(--spacer-12)]">
 <button type="button" class="ds-btn ds-btn--secondary ds-btn--block" @click="showEditor = false">取消</button>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--block" @click="submit">创建</button>
 </div>
 </div>
 </DsPopup>
 </main>
</template>
