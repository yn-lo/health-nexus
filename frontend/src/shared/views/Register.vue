<script setup lang="ts">
/**
 * Register 注册页 — 「极简编辑风」上下分屏设计（对齐 Login）
 *
 * 后端契约：POST /api/auth/register { username, password }（DisallowUnknownFields）
 * 上半屏品牌叙事（视觉重心），下半屏注册表单（功能）
 * 无卡片容器 + 下划线输入框 + 胶囊按钮，与 Login 统一设计语言
 * 不写组件级 scoped 样式（遵循 styling.md 规则 1）
 *
 * UI/UX（ui-ux-pro-max §Forms）：aria-label / 密码显隐 / 实时强度 / 内联不一致提示 / loading+toast
 */
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { CircleAlert } from '@lucide/vue'
import { useDsToast } from '@/shared/composables/useDsToast'
import { PageShell, AuthHero, PasswordStrength, DsPasswordField, DsSubmitButton } from '@/shared/components'
import { useAuthStore } from '@/stores/auth'
import { errmsg } from '@/shared/api/client'

const router = useRouter()
const authStore = useAuthStore()
const { showFailToast, showSuccessToast } = useDsToast()

const username = ref('')
const password = ref('')
const confirmPassword = ref('')
const inviteCode = ref('')
const agreed = ref(false)
const loading = ref(false)
const errorMsg = ref('')

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
 if (!inviteCode.value.trim()) {
  errorMsg.value = '请输入邀请码'
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
  invite_code: inviteCode.value.trim(),
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
  :padded="false"
  background="var(--auth-bg)"
  class="auth-page relative overflow-hidden"
 >
  <div class="auth-aura auth-aura--top" aria-hidden="true" />

  <div class="auth-scroll relative mx-auto flex min-h-dvh w-full max-w-[400px] flex-col px-[var(--spacer-24)]">
   <!-- 上半屏：品牌叙事区（视觉重心） -->
   <AuthHero>
    <template #title>创建您的<br>健康助手账户</template>
    <template #subtitle>填写邀请码、用户名与密码，<br>即可开始使用</template>
   </AuthHero>

   <!-- 下半屏：注册表单区（功能） -->
   <section class="auth-form-area flex flex-1 flex-col justify-center pb-[var(--spacer-16)]">
    <form class="auth-form flex flex-col gap-[var(--spacer-16)]" @submit.prevent="handleRegister">
     <!-- 用户名 -->
     <div class="auth-field">
      <label class="auth-label" for="reg-username">用户名</label>
      <div class="ds-field-wrap ds-field-wrap--underline">
       <input id="reg-username" v-model="username" type="text" placeholder="设置用户名" autocomplete="username" aria-label="用户名">
      </div>
     </div>

     <!-- 密码 -->
     <div class="auth-field">
      <label class="auth-label">密码</label>
      <DsPasswordField v-model="password" placeholder="设置密码(8-20位)" autocomplete="new-password" aria-label="密码">
       <PasswordStrength :password="password" :segments="4" />
      </DsPasswordField>
     </div>

     <!-- 确认密码 -->
     <div class="auth-field">
      <label class="auth-label">确认密码</label>
      <DsPasswordField v-model="confirmPassword" placeholder="确认密码" autocomplete="new-password" aria-label="确认密码" :error="passwordMismatch">
       <p
        v-if="passwordMismatch"
        class="text-body-sm text-[var(--status-error-default)]"
       >
        两次输入的密码不一致
       </p>
      </DsPasswordField>
     </div>

     <!-- 邀请码 -->
     <div class="auth-field">
      <label class="auth-label" for="reg-invite">邀请码</label>
      <div class="ds-field-wrap ds-field-wrap--underline">
       <input id="reg-invite" v-model="inviteCode" type="text" inputmode="numeric" autocomplete="off" maxlength="6" placeholder="请输入 6 位邀请码" aria-label="邀请码">
      </div>
      <p class="text-body-xs text-text-tertiary">注册本平台需要邀请码，请向管理员索取</p>
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
     <DsSubmitButton :loading="loading" text="注 册" class="auth-submit-btn" />
    </form>

    <!-- 底部登录链接 -->
    <footer class="auth-footer mt-[var(--spacer-24)] flex flex-col items-center gap-[var(--spacer-12)] text-center">
     <p class="text-body-base text-text-secondary">
      已有账号？
      <button class="ds-link-btn font-medium" @click="goLogin">立即登录</button>
     </p>
    </footer>
   </section>
  </div>
 </PageShell>
</template>
