<script setup lang="ts">
/**
 * Register 注册页 — 对齐 Login/ForgotPassword/ChangePassword 统一规范
 *
 * 后端契约：POST /api/auth/register { username, password }（DisallowUnknownFields）
 * 渐变背景 + 品牌区(BrandLogo md) + 注册卡片（用户名/密码/确认密码/协议）
 * 不写组件级 scoped 样式（遵循 styling.md 规则 1），字段高度/圆角由全局 @layer components 统一
 *
 * UI/UX（ui-ux-pro-max §Forms）：aria-label / 密码显隐 / 实时强度 / 内联不一致提示 / loading+toast
 */
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { User, Lock, Eye, EyeOff, CircleAlert } from '@lucide/vue'
import { useDsToast } from '@/shared/composables/useDsToast'
import { PageShell, BrandLogo, PasswordStrength } from '@/shared/components'
import { useAuthStore } from '@/stores/auth'
import { errmsg } from '@/shared/api/client'

const router = useRouter()
const authStore = useAuthStore()
const { showFailToast, showSuccessToast } = useDsToast()

const username = ref('')
const password = ref('')
const confirmPassword = ref('')
const agreed = ref(false)
const loading = ref(false)
const errorMsg = ref('')

/* 密码显隐 */
const showPassword = ref(false)
const showConfirmPassword = ref(false)

const passwordMismatch = computed(
 () => confirmPassword.value.length > 0 && password.value !== confirmPassword.value,
)

async function handleRegister() {
 errorMsg.value = ''

 if (!username.value.trim()) {
 errorMsg.value = '请输入用户名'
 showFailToast(errorMsg.value)
 return
 }
 if (!password.value) {
 errorMsg.value = '请输入密码'
 showFailToast(errorMsg.value)
 return
 }
 if (password.value !== confirmPassword.value) {
 errorMsg.value = '两次输入的密码不一致'
 showFailToast(errorMsg.value)
 return
 }
 if (!agreed.value) {
 errorMsg.value = '请阅读并同意用户协议和隐私政策'
 showFailToast(errorMsg.value)
 return
 }

 loading.value = true
 try {
 await authStore.register({
 username: username.value,
 password: password.value,
 })
 showSuccessToast('注册成功')
 await router.push('/chat')
 } catch (e) {
 errorMsg.value = errmsg(e, '注册失败，请稍后重试')
 showFailToast(errorMsg.value)
 } finally {
 loading.value = false
 }
}

function goLogin() {
 router.push({ name: 'login' })
}
</script>

<template>
 <PageShell
 :bottom-nav="false"
 :padded="true"
 background="linear-gradient(180deg, var(--bg-base-secondary) 0%, var(--bg-brand-popup) 100%)"
 class="flex items-center justify-center py-[var(--spacer-32)]"
 >
 <div class="flex w-full max-w-[400px] flex-col items-center gap-[var(--spacer-24)]">
 <!-- 品牌区 -->
 <div class="flex flex-col items-center gap-[var(--spacer-8)] text-center">
 <BrandLogo size="md" />
 <p class="text-body-base text-text-secondary max-w-[280px]">
 创建您的健康助手账户
 </p>
 </div>

 <!-- 注册卡片 -->
 <div class="flex w-full flex-col gap-[var(--spacer-20)] rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] p-[var(--spacer-24)] border border-[var(--border-neutral-l1)] shadow-[var(--shadow-sm)]">
 <!-- 标题 -->
 <div>
 <h2 class="font-heading text-heading-lg font-semibold text-text">
 注册
 </h2>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-secondary">
 设置用户名与密码即可开始使用
 </p>
 </div>

 <!-- 表单 -->
 <form class="flex flex-col gap-[var(--spacer-16)]" @submit.prevent="handleRegister">
 <!-- 用户名 -->
 <div class="ds-field-wrap ds-field-wrap--secondary">
 <User class="h-4 w-4 shrink-0 text-icon-tertiary" />
 <input v-model="username" type="text" placeholder="设置用户名" autocomplete="username" aria-label="用户名">
 </div>

 <!-- 密码 -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <div class="ds-field-wrap ds-field-wrap--secondary">
 <Lock class="h-4 w-4 shrink-0 text-icon-tertiary" />
 <input v-model="password" :type="showPassword ? 'text' : 'password'" placeholder="设置密码(8-20位)" autocomplete="new-password" aria-label="密码">
 <button type="button" class="inline-flex h-6 w-6 shrink-0 items-center justify-center p-0 text-icon-tertiary hover:text-icon" :aria-label="showPassword ? '隐藏密码' : '显示密码'" @click="showPassword = !showPassword">
 <Eye v-if="!showPassword" class="h-4 w-4" />
 <EyeOff v-else class="h-4 w-4" />
 </button>
 </div>
 <!-- 密码强度指示器 -->
 <PasswordStrength :password="password" :segments="4" />
 </div>

 <!-- 确认密码 -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <div class="ds-field-wrap ds-field-wrap--secondary" :class="{ 'ds-field-wrap--error': passwordMismatch }">
 <Lock class="h-4 w-4 shrink-0 text-icon-tertiary" />
 <input v-model="confirmPassword" :type="showConfirmPassword ? 'text' : 'password'" placeholder="确认密码" autocomplete="new-password" aria-label="确认密码">
 <button type="button" class="inline-flex h-6 w-6 shrink-0 items-center justify-center p-0 text-icon-tertiary hover:text-icon" :aria-label="showConfirmPassword ? '隐藏密码' : '显示密码'" @click="showConfirmPassword = !showConfirmPassword">
 <Eye v-if="!showConfirmPassword" class="h-4 w-4" />
 <EyeOff v-else class="h-4 w-4" />
 </button>
 </div>
 <p
 v-if="passwordMismatch"
 class="text-body-sm text-[var(--status-error-default)]"
 >
 两次输入的密码不一致
 </p>
 </div>

 <!-- 错误提示 -->
 <div
 v-if="errorMsg"
 class="ds-alert ds-alert--error"
 role="alert"
 >
 <CircleAlert class="icon" />
 <span>{{ errorMsg }}</span>
 </div>

 <!-- 协议 -->
 <label class="flex cursor-pointer items-center gap-[var(--spacer-8)]">
 <span class="ds-checkbox">
 <input v-model="agreed" type="checkbox" class="ds-checkbox__input">
 <span class="ds-checkbox__box" />
 </span>
 <span class="text-body-md text-text-secondary">
      我已阅读并同意
      <a href="/terms" class="text-text-brand hover:text-text-brand-hover">《用户协议》</a>
      和
      <a href="/privacy" class="text-text-brand hover:text-text-brand-hover">《隐私政策》</a>
      </span>
 </label>

 <!-- 注册按钮 -->
 <button
 type="submit"
 class="ds-btn ds-btn--primary ds-btn--block"
 :class="{ 'ds-btn--loading': loading }"
 :disabled="loading"
 >
 <span v-if="loading" class="ds-btn__spinner" />
 注册
 </button>
 </form>
 </div>

 <!-- 底部登录链接 -->
 <footer class="flex flex-col items-center gap-[var(--spacer-8)] text-center">
 <p class="text-body-md text-text-tertiary">
 已有账号？
 <button
 class="ds-link-btn font-medium"
 @click="goLogin"
>立即登录</button>
 </p>
 </footer>
 </div>
 </PageShell>
</template>
