<script setup lang="ts">
/**
 * 账号详情 — 查看账户信息 + 重置密码 + 锁定/解锁 + 删除
 * API: authApi.listStaffAccounts/resetStaffAccountPassword/lockStaffAccount/unlockStaffAccount/deleteStaffAccount
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { UserCog, Shield, Lock, Unlock, RotateCcw, Trash2, Eye, EyeOff } from '@lucide/vue'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { AppHeader, PasswordStrength, DsPopup } from '@/shared/components'
import { authApi, errmsg, getUserStored, fmtDateTime } from '@/shared'
import { ROLE_LABEL } from '@/shared/constants/roles'
import type { StaffAccount } from '@/shared'

const router = useRouter()
const route = useRoute()
const currentUserId = getUserStored()?.id ?? 0
const { showSuccessToast, showFailToast } = useDsToast()
const { showConfirmDialog } = useDsDialog()

const account = ref<StaffAccount | null>(null)
const loading = ref(false)

const accountId = computed(() => Number(route.params.id))
const isSelf = computed(() => account.value?.id === currentUserId)

async function load() {
  loading.value = true
  try {
    const res = await authApi.listStaffAccounts({ page: 1, page_size: 50 })
    account.value = res.items.find((a) => a.id === accountId.value) ?? null
  } catch (e) {
    showFailToast(errmsg(e, '加载失败'))
  } finally {
    loading.value = false
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

onMounted(load)
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
              <span class="text-body-sm" :class="account.is_active ? 'text-[var(--status-success-default)]' : 'text-text-tertiary'">
                {{ account.is_active ? '正常' : '已锁定' }}
              </span>
            </div>
          </div>
        </div>
      </section>

      <section class="px-[var(--spacer-16)] pt-[var(--spacer-24)]">
        <h3 class="mb-[var(--spacer-8)] text-body-sm font-medium text-text-tertiary">账户信息</h3>
        <div class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
          <div class="ds-list-item ds-list-item--divider">
            <span class="ds-list-item__icon">
              <Shield :size="20" class="text-icon-secondary" />
            </span>
            <div class="ds-list-item__content">
              <span class="ds-list-item__title">角色</span>
              <span class="ds-list-item__meta">{{ ROLE_LABEL[account.role] }}</span>
            </div>
          </div>
          <div class="ds-list-item ds-list-item--divider">
            <span class="ds-list-item__icon">
              <Lock v-if="account.is_active" :size="20" class="text-[var(--status-success-default)]" />
              <Unlock v-else :size="20" class="text-icon-secondary" />
            </span>
            <div class="ds-list-item__content">
              <span class="ds-list-item__title">账户状态</span>
              <span class="ds-list-item__meta">{{ account.is_active ? '正常' : '已锁定' }}</span>
            </div>
            <div class="ds-list-item__trailing">
              <label class="ds-switch ds-switch--sm" :class="{ 'pointer-events-none opacity-50': isSelf }">
                <input type="checkbox" class="ds-switch__input" :checked="account.is_active" :disabled="isSelf" @change="toggleLock">
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
        <button type="button" class="ds-btn ds-btn--danger-outline ds-btn--block" @click="deleteAccount">
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
          <span class="text-body-sm text-text-secondary">新密码</span>
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
  </main>
</template>
