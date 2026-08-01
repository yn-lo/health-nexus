<script setup lang="ts">
/**
 * ChangePassword 修改密码页 — 已登录用户
 *
 * 后端契约：POST /api/auth/change-password { old_password, new_password }
 * userID 由 JWT 注入，校验旧密码正确性 + 新密码强度
 *
 * 双端共享：患者端 (/chat/change-password) 与医护端 (/staff/change-password) 复用
 * UI/UX（ui-ux-pro-max §Forms）：密码显隐 / 实时强度 / 内联不一致提示 / loading+toast
 */
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { CircleAlert } from '@lucide/vue'
import { useDsToast } from '@/shared/composables/useDsToast'
import { PageShell, AppHeader, PasswordStrength, DsPasswordField, DsSubmitButton } from '@/shared/components'
import { useAuthStore } from '@/stores/auth'
import { errmsg } from '@/shared/api/client'

const router = useRouter()
const authStore = useAuthStore()
const { showFailToast, showSuccessToast } = useDsToast()

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const loading = ref(false)
const errorMsg = ref('')

const passwordMismatch = computed(
 () => confirmPassword.value.length > 0 && newPassword.value !== confirmPassword.value,
)

/** 新旧密码相同提示（防止无效修改） */
const sameAsOld = computed(
 () => oldPassword.value.length > 0 && newPassword.value.length > 0 && oldPassword.value === newPassword.value,
)

function goBack() {
 router.back()
}

async function handleChange() {
 errorMsg.value = ''
 if (!oldPassword.value) {
 errorMsg.value = '请输入原密码'
 showFailToast(errorMsg.value)
 return
 }
 if (!newPassword.value) {
 errorMsg.value = '请输入新密码'
 showFailToast(errorMsg.value)
 return
 }
 if (sameAsOld.value) {
 errorMsg.value = '新密码不能与原密码相同'
 showFailToast(errorMsg.value)
 return
 }
 if (newPassword.value !== confirmPassword.value) {
 errorMsg.value = '两次输入的密码不一致'
 showFailToast(errorMsg.value)
 return
 }
 loading.value = true
 try {
 await authStore.changePassword(oldPassword.value, newPassword.value)
 showSuccessToast('密码修改成功')
 router.back()
 } catch (e) {
 errorMsg.value = errmsg(e, '修改失败，请稍后重试')
 showFailToast(errorMsg.value)
 } finally {
 loading.value = false
 }
}
</script>

<template>
 <PageShell
 :bottom-nav="false"
 :padded="false"
 background="var(--bg-base-secondary)"
 >
 <!-- sticky 顶栏：返回按钮独立于内容容器，贴屏幕左缘 -->
 <AppHeader title="修改密码" @back="goBack" />

 <div class="flex flex-col gap-[var(--spacer-24)] px-[var(--spacer-16)] pt-[var(--spacer-24)] pb-[var(--spacer-32)]">
 <!-- 标题区 -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <h1 class="font-heading text-heading-lg font-semibold text-text">
 修改密码
 </h1>
 <p class="text-body-base text-text-secondary">
 为保障账户安全，请定期更新密码
 </p>
 </div>

 <!-- 表单卡片 -->
 <div class="flex flex-col gap-[var(--spacer-20)] rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] p-[var(--spacer-24)] border border-[var(--border-neutral-l1)] shadow-[var(--shadow-sm)]">
 <form class="flex flex-col gap-[var(--spacer-16)]" @submit.prevent="handleChange">
 <!-- 原密码 -->
 <DsPasswordField v-model="oldPassword" label="原密码" placeholder="请输入原密码" autocomplete="current-password" aria-label="原密码" />

 <!-- 新密码 -->
 <DsPasswordField v-model="newPassword" label="新密码" placeholder="设置新密码(8-20位)" autocomplete="new-password" aria-label="新密码">
  <PasswordStrength :password="newPassword" :segments="4" />
  <p
   v-if="sameAsOld"
   class="text-body-sm text-[var(--status-error-default)]"
  >
   新密码不能与原密码相同
  </p>
 </DsPasswordField>

 <!-- 确认新密码 -->
 <DsPasswordField v-model="confirmPassword" label="确认新密码" placeholder="请再次输入新密码" autocomplete="new-password" aria-label="确认新密码" :error="passwordMismatch">
  <p
   v-if="passwordMismatch"
   class="text-body-sm text-[var(--status-error-default)]"
  >
   两次输入的密码不一致
  </p>
 </DsPasswordField>

 <!-- 错误提示 -->
 <div
  v-if="errorMsg"
  class="ds-alert ds-alert--error"
  role="alert"
 >
  <CircleAlert class="icon" />
  <span>{{ errorMsg }}</span>
 </div>

 <DsSubmitButton :loading="loading" text="确认修改" />
 </form>
 </div>
 </div>
 </PageShell>
</template>
