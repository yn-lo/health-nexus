<script setup lang="ts">
/**
 * ForgotPassword 忘记密码页 — 两步重置流程
 *
 * 后端契约：
 * 1. POST /api/auth/password-reset/request { username }
 * 始终返回 success（安全设计：不泄露用户是否存在），生成 15 分钟有效的重置 token
 * 2. POST /api/auth/password-reset/confirm { token, new_password }
 * 校验 token 一次性使用 + 新密码强度
 *
 * UI/UX（ui-ux-pro-max §Forms）：
 * - 分步指示器明确当前阶段
 * - 密码显隐切换 / 实时强度指示
 * - 提交 loading + 成功/失败 toast
 * - 触摸目标 ≥44px / focus-visible 焦点环 / aria-label
 */
import { ref, computed } from 'vue'
import { User, KeyRound, Lock, Eye, EyeOff, CircleAlert, CheckCircle2, ShieldCheck } from '@lucide/vue'
import { useDsToast } from '@/shared/composables/useDsToast'
import { PageShell, BrandLogo, PasswordStrength } from '@/shared/components'
import { requestPasswordReset, confirmPasswordReset } from '@/shared/api/auth'
import { errmsg } from '@/shared/api/client'

const { showFailToast, showSuccessToast } = useDsToast()

/** 当前步骤：1=请求重置 / 2=确认重置 / 3=完成 */
const step = ref<1 | 2 | 3>(1)

/* ── Step 1：请求重置 ── */
const username = ref('')
const requesting = ref(false)
const requestError = ref('')

/* ── Step 2：确认重置 ── */
const token = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const showNewPassword = ref(false)
const showConfirmPassword = ref(false)
const confirming = ref(false)
const confirmError = ref('')

const passwordMismatch = computed(
 () => confirmPassword.value.length > 0 && newPassword.value !== confirmPassword.value,
)

function goLogin() {
 // 统一登录页在 /login（chat SPA），需跨 MPA 跳转
 window.location.href = '/login' // ponytail:allow-location 跨 MPA 跳转
}

async function handleRequest() {
 requestError.value = ''
 if (!username.value.trim()) {
 requestError.value = '请输入用户名'
 showFailToast(requestError.value)
 return
 }
 requesting.value = true
 try {
 await requestPasswordReset(username.value.trim())
 step.value = 2
 } catch (e) {
 requestError.value = errmsg(e, '请求失败，请稍后重试')
 showFailToast(requestError.value)
 } finally {
 requesting.value = false
 }
}

async function handleConfirm() {
 confirmError.value = ''
 if (!token.value.trim()) {
 confirmError.value = '请输入重置令牌'
 showFailToast(confirmError.value)
 return
 }
 if (!newPassword.value) {
 confirmError.value = '请输入新密码'
 showFailToast(confirmError.value)
 return
 }
 if (newPassword.value !== confirmPassword.value) {
 confirmError.value = '两次输入的密码不一致'
 showFailToast(confirmError.value)
 return
 }
 confirming.value = true
 try {
 await confirmPasswordReset(token.value.trim(), newPassword.value)
 showSuccessToast('密码重置成功')
 step.value = 3
 } catch (e) {
 confirmError.value = errmsg(e, '重置失败，请检查令牌是否正确')
 showFailToast(confirmError.value)
 } finally {
 confirming.value = false
 }
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
 <BrandLogo size="md" />

 <!-- 步骤指示器 -->
 <div class="flex items-center gap-[var(--spacer-8)]" aria-hidden="true">
 <div
 v-for="s in [1, 2, 3]"
 :key="s"
 class="h-1 rounded-[var(--radius-full)] transition-colors duration-[var(--duration-normal)] ease-[var(--ease-out)]"
 :class="step >= (s as 1 | 2 | 3)
 ? 'w-8 bg-[var(--bg-brand)]'
 : 'w-4 bg-[var(--bg-overlay-l3)]'"
 />
 </div>

 <!-- 内容卡片 -->
 <div class="flex w-full flex-col gap-[var(--spacer-20)] rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] p-[var(--spacer-24)] border border-[var(--border-neutral-l1)] shadow-[var(--shadow-sm)]">
 <!-- 图标 + 标题 -->
 <div class="flex flex-col items-center gap-[var(--spacer-12)] text-center">
 <div class="flex items-center justify-center w-16 h-16 rounded-full bg-[var(--bg-brand-popup)]">
 <ShieldCheck :size="32" class="text-text-brand" />
 </div>
 <div>
 <h2 class="font-heading text-heading-md font-semibold text-text">
 {{ step === 1 ? '找回密码' : step === 2 ? '设置新密码' : '重置完成' }}
 </h2>
 <p class="mt-[var(--spacer-8)] text-body-base text-text-secondary">
 <template v-if="step === 1">输入用户名，我们将为您生成重置令牌</template>
 <template v-else-if="step === 2">输入收到的重置令牌与新密码</template>
 <template v-else>请使用新密码登录</template>
 </p>
 </div>
 </div>

 <!-- Step 1：请求重置 -->
 <form v-if="step === 1" class="flex flex-col gap-[var(--spacer-16)]" @submit.prevent="handleRequest">
 <div class="ds-field-wrap ds-field-wrap--secondary">
 <User class="h-4 w-4 shrink-0 text-icon-tertiary" />
 <input v-model="username" type="text" placeholder="请输入用户名" autocomplete="username" aria-label="用户名">
 </div>

 <div
 v-if="requestError"
 class="ds-alert ds-alert--error"
 role="alert"
 >
 <CircleAlert class="icon" />
 <span>{{ requestError }}</span>
 </div>

 <button
 type="submit"
 class="ds-btn ds-btn--primary ds-btn--block"
 :class="{ 'ds-btn--loading': requesting }"
 :disabled="requesting"
 >
 <span v-if="requesting" class="ds-btn__spinner" />
 获取重置令牌
 </button>
 </form>

 <!-- Step 2：确认重置 -->
 <form v-else-if="step === 2" class="flex flex-col gap-[var(--spacer-16)]" @submit.prevent="handleConfirm">
 <!-- 重置令牌 -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <div class="ds-field-wrap ds-field-wrap--secondary">
 <KeyRound class="h-4 w-4 shrink-0 text-icon-tertiary" />
 <input v-model="token" type="text" placeholder="重置令牌" autocomplete="off" aria-label="重置令牌">
 </div>
 <p class="text-body-sm text-text-tertiary">
 令牌已发送至您的注册联系方式，15 分钟内有效
 </p>
 </div>

 <!-- 新密码 -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <div class="ds-field-wrap ds-field-wrap--secondary">
 <Lock class="h-4 w-4 shrink-0 text-icon-tertiary" />
 <input v-model="newPassword" :type="showNewPassword ? 'text' : 'password'" placeholder="设置新密码(8-20位)" autocomplete="new-password" aria-label="新密码">
 <button type="button" class="inline-flex h-6 w-6 shrink-0 items-center justify-center p-0 text-icon-tertiary hover:text-icon" :aria-label="showNewPassword ? '隐藏密码' : '显示密码'" @click="showNewPassword = !showNewPassword">
 <Eye v-if="!showNewPassword" class="h-4 w-4" />
 <EyeOff v-else class="h-4 w-4" />
 </button>
 </div>
 <PasswordStrength :password="newPassword" :segments="4" />
 </div>

 <!-- 确认新密码 -->
 <div class="ds-field-wrap ds-field-wrap--secondary" :class="{ 'ds-field-wrap--error': passwordMismatch }">
 <Lock class="h-4 w-4 shrink-0 text-icon-tertiary" />
 <input v-model="confirmPassword" :type="showConfirmPassword ? 'text' : 'password'" placeholder="确认新密码" autocomplete="new-password" aria-label="确认新密码">
 <button type="button" class="inline-flex h-6 w-6 shrink-0 items-center justify-center p-0 text-icon-tertiary hover:text-icon" :aria-label="showConfirmPassword ? '隐藏密码' : '显示密码'" @click="showConfirmPassword = !showConfirmPassword">
 <Eye v-if="!showConfirmPassword" class="h-4 w-4" />
 <EyeOff v-else class="h-4 w-4" />
 </button>
 </div>
 <p
 v-if="passwordMismatch"
 class="-mt-[var(--spacer-8)] text-body-sm text-[var(--status-error-default)]"
 >
 两次输入的密码不一致
 </p>

 <!-- 错误提示 -->
 <div
 v-if="confirmError"
 class="ds-alert ds-alert--error"
 role="alert"
 >
 <CircleAlert class="icon" />
 <span>{{ confirmError }}</span>
 </div>

 <button
 type="submit"
 class="ds-btn ds-btn--primary ds-btn--block"
 :class="{ 'ds-btn--loading': confirming }"
 :disabled="confirming"
 >
 <span v-if="confirming" class="ds-btn__spinner" />
 重置密码
 </button>
 </form>

 <!-- Step 3：完成 -->
 <div v-else class="flex flex-col items-center gap-[var(--spacer-20)]">
 <div class="flex items-center justify-center w-16 h-16 rounded-full bg-[var(--status-success-surface-l1)]">
 <CheckCircle2 :size="32" class="text-[var(--status-success-default)]" />
 </div>
 <p class="text-center text-body-base text-text-secondary">
 您的密码已重置成功，请使用新密码登录
 </p>
 <button
 class="ds-btn ds-btn--primary ds-btn--block"
 @click="goLogin"
 >
 前往登录
 </button>
 </div>

 <!-- 兜底提示：医疗场景下令牌可能需管理员协助 -->
 <div v-if="step !== 3" class="ds-alert ds-alert--warning">
 <ShieldCheck class="icon" />
 <p class="text-body-sm ">
 未收到令牌？请联系医院信息科或携带有效证件前往服务窗口办理
 </p>
 </div>
 </div>

 <!-- 底部登录链接 -->
 <footer v-if="step !== 3" class="flex flex-col items-center gap-[var(--spacer-8)] text-center">
 <p class="text-body-md text-text-tertiary">
 想起密码了？
 <button
 class="ds-link-btn font-medium"
 @click="goLogin"
>立即登录</button>
 </p>
 </footer>
 </div>
 </PageShell>
</template>
