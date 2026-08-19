<script setup lang="ts">
/**
 * 账号详情 — 查看账户信息 + 重置密码 + 锁定/解锁 + 删除
 * API: authApi.listStaffAccounts/resetStaffAccountPassword/lockStaffAccount/unlockStaffAccount/deleteStaffAccount
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { UserCog, Shield, Building2, Lock, Unlock, RotateCcw, Trash2, Eye, EyeOff, Pencil, RotateCcwSquare } from '@lucide/vue'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { AppHeader, PasswordStrength, DsPopup } from '@/shared/components'
import { authApi, errmsg, getUserStored, fmtDateTime, useDepartmentOptions } from '@/shared'
import { ROLE_LABEL, SUPER_ADMIN_ROLE, STAFF_ROLES, PATIENT_ROLES, type UserRole } from '@/shared/constants/roles'
import type { StaffAccount } from '@/shared'

const router = useRouter()
const route = useRoute()
const currentUser = getUserStored()
const currentUserId = currentUser?.id ?? 0
/** 是否超管：超管可改任意账户科室；科室管理员仅本科室账户且只能改成本科室 */
const isSuperAdmin = currentUser?.role === SUPER_ADMIN_ROLE
const currentDeptId = currentUser?.dept_id ?? 0
const { showSuccessToast, showFailToast } = useDsToast()
const { showConfirmDialog } = useDsDialog()

const account = ref<StaffAccount | null>(null)
const loading = ref(false)

const accountId = computed(() => Number(route.params.id))
const isSelf = computed(() => account.value?.id === currentUserId)

// 修改科室弹窗
const showDeptDialog = ref(false)
const deptOptions = ref<{ id: number | null; label: string }[]>([])
const selectedDeptId = ref(0)
const savingDept = ref(false)

async function load() {
  loading.value = true
  try {
    // 超管需包含已删除用户，否则已删除账户详情无法加载（无法恢复）
    const res = await authApi.listStaffAccounts(isSuperAdmin ? { page: 1, page_size: 50, include_deleted: true } : { page: 1, page_size: 50 })
    account.value = res.items.find((a) => a.id === accountId.value) ?? null
  } catch (e) {
    showFailToast(errmsg(e, '加载失败'))
  } finally {
    loading.value = false
  }
}

async function loadDepts() {
  const { options, load } = useDepartmentOptions()
  await load()
  deptOptions.value = options.value
}

function openDeptDialog() {
  if (!account.value) return
  selectedDeptId.value = account.value.primary_dept_id
  showDeptDialog.value = true
}

async function submitDept() {
  if (!account.value) return
  if (!selectedDeptId.value) {
    showFailToast('请选择科室')
    return
  }
  savingDept.value = true
  try {
    const updated = await authApi.updateStaffAccountDept(account.value.id, selectedDeptId.value)
    account.value = updated
    showSuccessToast('科室已更新')
    showDeptDialog.value = false
  } catch (e) {
    showFailToast(errmsg(e, '更新失败'))
  } finally {
    savingDept.value = false
  }
}

// 修改角色弹窗（仅超管）
const showRoleDialog = ref(false)
const selectedRole = ref<UserRole>(STAFF_ROLES[STAFF_ROLES.length - 1])
const savingRole = ref(false)

/** 全部角色（STAFF + PATIENT），顺序：管理员 → 医护 → 患者 */
const ALL_ROLES: UserRole[] = [...STAFF_ROLES, ...PATIENT_ROLES]

function openRoleDialog() {
  if (!account.value) return
  selectedRole.value = account.value.role
  showRoleDialog.value = true
}

async function submitRole() {
  if (!account.value) return
  savingRole.value = true
  try {
    const updated = await authApi.updateStaffAccountRole(account.value.id, selectedRole.value)
    account.value = updated
    showSuccessToast('角色已更新')
    showRoleDialog.value = false
  } catch (e) {
    showFailToast(errmsg(e, '更新失败'))
  } finally {
    savingRole.value = false
  }
}

// 恢复软删除账户（仅超管）
const restoring = ref(false)

async function restoreAccount() {
  if (!account.value) return
  try {
    await showConfirmDialog({
      title: '确认恢复',
      message: `确定要恢复账户 ${account.value.username} 吗？恢复后该账户将重新启用。`,
    })
  } catch {
    return
  }
  restoring.value = true
  try {
    const updated = await authApi.restoreStaffAccount(account.value.id)
    account.value = updated
    showSuccessToast('已恢复')
  } catch (e) {
    showFailToast(errmsg(e, '恢复失败'))
  } finally {
    restoring.value = false
  }
}

async function toggleLock() {
  if (!account.value) return
  try {
    if (account.value.is_active) {
      await showConfirmDialog({ title: '确认锁定', message: `确定要锁定账户 ${account.value.username} 吗？` })
      await authApi.lockStaffAccount(account.value.id)
      account.value.is_active = false
      showSuccessToast('已锁定')
    } else {
      await authApi.unlockStaffAccount(account.value.id)
      account.value.is_active = true
      showSuccessToast('已解锁')
    }
  } catch (e: unknown) {
    if (e === 'cancel') return
    showFailToast(errmsg(e, '操作失败'))
  }
}

const showResetDialog = ref(false)
const showPassword = ref(false)
const newPassword = ref('')
const resetting = ref(false)

function openResetDialog() {
  newPassword.value = ''
  showResetDialog.value = true
}

async function submitReset() {
  if (!account.value) return
  if (newPassword.value.length < 8 || !/\p{L}/u.test(newPassword.value) || !/\p{N}/u.test(newPassword.value)) {
    showFailToast('密码至少 8 位，需含字母和数字')
    return
  }
  resetting.value = true
  try {
    await authApi.resetStaffAccountPassword(account.value.id, newPassword.value)
    showSuccessToast('密码已重置')
    showResetDialog.value = false
    newPassword.value = ''
  } catch (e) {
    showFailToast(errmsg(e, '重置失败'))
  } finally {
    resetting.value = false
  }
}

async function deleteAccount() {
  if (!account.value) return
  try {
    await showConfirmDialog({
      title: '确认删除',
      message: `确定要删除账户 ${account.value.username} 吗？此操作不可恢复。`,
      danger: true,
    })
  } catch {
    return
  }
  try {
    await authApi.deleteStaffAccount(account.value.id)
    showSuccessToast('已删除')
    router.replace({ name: 'staff-config-accounts' })
  } catch (e) {
    showFailToast(errmsg(e, '删除失败'))
  }
}

onMounted(() => {
  load()
  loadDepts()
})
</script>

<template>
  <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
    <AppHeader title="账号详情" @back="router.back" />

    <div v-if="loading" class="flex items-center justify-center py-20">
      <div class="ds-loading">
        <span class="ds-loading__spinner" />
        <span class="ds-loading__text">加载中</span>
      </div>
    </div>

    <template v-else-if="account">
      <section class="px-[var(--spacer-16)] pt-[var(--spacer-16)]">
        <div class="flex items-center gap-[var(--spacer-16)]">
          <span class="flex h-14 w-14 items-center justify-center rounded-[var(--radius-full)] bg-[var(--ai-gradient-soft)]">
            <UserCog :size="28" class="text-icon-brand" />
          </span>
          <div>
            <h1 class="text-heading-sm font-semibold text-text">
              {{ account.username }}<span v-if="isSelf" class="ml-[var(--spacer-4)] text-body-xs text-text-tertiary">（我）</span>
            </h1>
            <div class="mt-[var(--spacer-4)] flex items-center gap-[var(--spacer-8)]">
              <span class="ds-tag ds-tag--primary ds-tag--plain">{{ ROLE_LABEL[account.role] }}</span>
              <span class="text-body-sm" :class="account.is_deleted ? 'text-[var(--status-error-default)]' : (account.is_active ? 'text-[var(--status-success-default)]' : 'text-text-tertiary')">
                {{ account.is_deleted ? '已删除' : (account.is_active ? '正常' : '已锁定') }}
              </span>
            </div>
          </div>
        </div>
      </section>

      <section class="px-[var(--spacer-16)] pt-[var(--spacer-24)]">
        <h3 class="mb-[var(--spacer-8)] text-body-sm font-medium text-text-tertiary">账户信息</h3>
        <div class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
          <div class="ds-list-item ds-list-item--divider" :class="{ 'cursor-pointer': isSuperAdmin && !isSelf }" @click="isSuperAdmin && !isSelf && openRoleDialog()">
            <span class="ds-list-item__icon">
              <Shield :size="20" class="text-icon-secondary" />
            </span>
            <div class="ds-list-item__content">
              <span class="ds-list-item__title">角色</span>
              <span class="ds-list-item__meta">{{ ROLE_LABEL[account.role] }}</span>
            </div>
            <div v-if="isSuperAdmin && !isSelf" class="ds-list-item__trailing">
              <Pencil :size="16" class="text-icon-tertiary" />
            </div>
          </div>
          <div class="ds-list-item ds-list-item--divider cursor-pointer" @click="openDeptDialog">
            <span class="ds-list-item__icon">
              <Building2 :size="20" class="text-icon-secondary" />
            </span>
            <div class="ds-list-item__content">
              <span class="ds-list-item__title">科室</span>
              <span class="ds-list-item__meta">{{ account.primary_dept_name || '未绑定' }}</span>
            </div>
            <div class="ds-list-item__trailing">
              <Pencil :size="16" class="text-icon-tertiary" />
            </div>
          </div>
          <div class="ds-list-item ds-list-item--divider">
            <span class="ds-list-item__icon">
              <Lock v-if="account.is_active && !account.is_deleted" :size="20" class="text-[var(--status-success-default)]" />
              <Unlock v-else :size="20" class="text-icon-secondary" />
            </span>
            <div class="ds-list-item__content">
              <span class="ds-list-item__title">账户状态</span>
              <span class="ds-list-item__meta">{{ account.is_deleted ? '已删除' : (account.is_active ? '正常' : '已锁定') }}</span>
            </div>
            <div class="ds-list-item__trailing">
              <label class="ds-switch ds-switch--sm" :class="{ 'pointer-events-none opacity-50': isSelf || account.is_deleted }">
                <input type="checkbox" class="ds-switch__input" :checked="account.is_active" :disabled="isSelf || account.is_deleted" @change="toggleLock">
                <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
              </label>
            </div>
          </div>
          <div class="ds-list-item">
            <span class="ds-list-item__icon">
              <RotateCcw :size="20" class="text-icon-secondary" />
            </span>
            <div class="ds-list-item__content">
              <span class="ds-list-item__title">密码</span>
              <span class="ds-list-item__meta">••••••••</span>
            </div>
            <div class="ds-list-item__trailing">
              <button type="button" class="ds-btn ds-btn--primary ds-btn--sm" :disabled="isSelf" @click="openResetDialog">重置</button>
            </div>
          </div>
        </div>
      </section>

      <section class="px-[var(--spacer-16)] pt-[var(--spacer-12)]">
        <h3 class="mb-[var(--spacer-8)] text-body-sm font-medium text-text-tertiary">系统信息</h3>
        <div class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
          <div class="ds-list-item">
            <span class="ds-list-item__content">
              <span class="ds-list-item__title">创建时间</span>
              <span class="ds-list-item__meta">{{ fmtDateTime(account.created_at) }}</span>
            </span>
          </div>
        </div>
      </section>

      <section v-if="!isSelf" class="px-[var(--spacer-16)] pt-[var(--spacer-24)]">
        <button v-if="account.is_deleted && isSuperAdmin" type="button" class="ds-btn ds-btn--primary ds-btn--block" :disabled="restoring" @click="restoreAccount">
          <RotateCcwSquare :size="16" />
          {{ restoring ? '恢复中…' : '恢复账户' }}
        </button>
        <button v-else type="button" class="ds-btn ds-btn--danger-outline ds-btn--block" @click="deleteAccount">
          <Trash2 :size="16" />
          删除账户
        </button>
      </section>
    </template>

    <div v-else class="flex flex-col items-center py-20">
      <span class="flex h-14 w-14 items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)]">
        <UserCog :size="28" class="text-icon-brand" />
      </span>
      <p class="mt-[var(--spacer-12)] text-body-base font-medium text-text">账户不存在</p>
      <p class="mt-[var(--spacer-4)] text-body-sm text-text-tertiary">该账户可能已被删除</p>
    </div>

    <DsPopup v-model:show="showResetDialog">
      <div class="p-[var(--spacer-16)] pb-[var(--spacer-24)]">
        <h3 class="mb-[var(--spacer-16)] text-heading-sm font-semibold text-text">
          重置密码
        </h3>
        <p class="mb-[var(--spacer-12)] text-body-sm text-text-secondary">
          为 <strong class="text-text">{{ account?.username }}</strong> 设置新密码，重置后旧密码立即失效。
        </p>
        <div class="flex flex-col gap-[var(--spacer-4)]">
          <span class="text-body-sm text-text-secondary">新密码<span class="text-[var(--status-error-default)]">*</span></span>
          <div class="ds-field-wrap">
            <input v-model="newPassword" :type="showPassword ? 'text' : 'password'" placeholder="至少 8 位，含字母和数字">
            <button type="button" class="inline-flex h-6 w-6 shrink-0 items-center justify-center p-0 text-icon-tertiary hover:text-icon" :aria-label="showPassword ? '隐藏密码' : '显示密码'" @click="showPassword = !showPassword">
              <Eye v-if="showPassword" class="h-4 w-4" />
              <EyeOff v-else class="h-4 w-4" />
            </button>
          </div>
        </div>
        <div class="mt-[var(--spacer-8)]">
          <PasswordStrength :password="newPassword" />
        </div>
        <div class="mt-[var(--spacer-16)] flex gap-[var(--spacer-12)]">
          <button type="button" class="ds-btn ds-btn--secondary ds-btn--block" @click="showResetDialog = false">取消</button>
          <button type="button" class="ds-btn ds-btn--primary ds-btn--block" :disabled="resetting" @click="submitReset">{{ resetting ? '重置中…' : '确认重置' }}</button>
        </div>
      </div>
    </DsPopup>

    <DsPopup v-model:show="showDeptDialog">
      <div class="p-[var(--spacer-16)] pb-[var(--spacer-24)]">
        <h3 class="mb-[var(--spacer-16)] text-heading-sm font-semibold text-text">
          修改科室
        </h3>
        <p class="mb-[var(--spacer-12)] text-body-sm text-text-secondary">
          为 <strong class="text-text">{{ account?.username }}</strong> 设置主科室。
        </p>
        <div class="flex flex-col gap-[var(--spacer-4)]">
          <span class="text-body-sm text-text-secondary">科室<span class="text-[var(--status-error-default)]">*</span></span>
          <select v-model.number="selectedDeptId" class="ds-input">
            <option :value="0" disabled>请选择科室</option>
            <option
              v-for="opt in deptOptions"
              :key="opt.id"
              :value="opt.id"
              :disabled="!isSuperAdmin && opt.id !== currentDeptId"
            >{{ opt.label }}</option>
          </select>
        </div>
        <div class="mt-[var(--spacer-16)] flex gap-[var(--spacer-12)]">
          <button type="button" class="ds-btn ds-btn--secondary ds-btn--block" @click="showDeptDialog = false">取消</button>
          <button type="button" class="ds-btn ds-btn--primary ds-btn--block" :disabled="savingDept" @click="submitDept">{{ savingDept ? '保存中…' : '保存' }}</button>
        </div>
      </div>
    </DsPopup>

    <DsPopup v-model:show="showRoleDialog">
      <div class="p-[var(--spacer-16)] pb-[var(--spacer-24)]">
        <h3 class="mb-[var(--spacer-16)] text-heading-sm font-semibold text-text">
          修改角色
        </h3>
        <p class="mb-[var(--spacer-12)] text-body-sm text-text-secondary">
          为 <strong class="text-text">{{ account?.username }}</strong> 设置新角色。
        </p>
        <div class="flex flex-col gap-[var(--spacer-8)]">
          <button
            v-for="opt in ALL_ROLES"
            :key="opt"
            type="button"
            class="ds-list-item ds-list-item--divider cursor-pointer"
            :class="{ 'text-text-brand': selectedRole === opt }"
            @click="selectedRole = opt"
          >
            <span class="ds-list-item__content">
              <span class="ds-list-item__title">{{ ROLE_LABEL[opt] }}</span>
            </span>
            <span v-if="selectedRole === opt" class="ds-list-item__trailing text-text-brand">✓</span>
          </button>
        </div>
        <div class="mt-[var(--spacer-16)] flex gap-[var(--spacer-12)]">
          <button type="button" class="ds-btn ds-btn--secondary ds-btn--block" @click="showRoleDialog = false">取消</button>
          <button type="button" class="ds-btn ds-btn--primary ds-btn--block" :disabled="savingRole" @click="submitRole">{{ savingRole ? '保存中…' : '保存' }}</button>
        </div>
      </div>
    </DsPopup>
  </main>
</template>
