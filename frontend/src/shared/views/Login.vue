<script setup lang="ts">
/**
 * Login 统一登录页 - AI Healthcare Native 统一风格
 *
 * 单一登录入口，不区分患者/医护。登录成功后按 user.role 自动跳转：
 * - PATIENT -> /chat
 * - SUPER_ADMIN/DEPT_ADMIN/DOCTOR/NURSE -> /staff
 *
 * 与 Register / ForgotPassword / ChangePassword 共用 PageShell + 设计令牌，
 * 不写组件级 scoped 样式（遵循 .harness/specs/conventions/styling.md 规则 1）。
 *
 * UI/UX（ui-ux-pro-max §Forms / §Accessibility）：
 * - 输入框 aria-label + 图标 + 显隐切换
 * - 提交 loading + 错误 banner + toast
 * - 触摸目标 ≥44px / focus-visible 全局焦点环
 */
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { User, CircleAlert, MessageCircle } from '@lucide/vue'
import { useDsToast } from '@/shared/composables/useDsToast'
import { PageShell, BrandLogo, DsPasswordField, DsSubmitButton } from '@/shared/components'
import { useAuthStore } from '@/stores/auth'
import { errmsg } from '@/shared/api/client'
import { STAFF_ROLES, type UserRole } from '@/shared/constants/roles'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const { showFailToast } = useDsToast()

const username = ref('')
const password = ref('')
const loading = ref(false)
const errorMsg = ref('')

/** 根据 role 判断跳转目标：医护角色 -> /staff，患者 -> /chat */
function homeByRole(role: UserRole): string {
 return STAFF_ROLES.includes(role) ? '/staff' : '/chat'
}

async function handleLogin() {
 if (!username.value.trim() || !password.value.trim()) {
 errorMsg.value = '请输入用户名和密码'
 return
 }
 errorMsg.value = ''
 loading.value = true
 try {
 const res = await authStore.login(username.value, password.value)
 const raw = typeof route.query.redirect === 'string' ? route.query.redirect : ''
 const isSafeRedirect = raw.startsWith('/')
 && !raw.startsWith('//')
 && !raw.startsWith('/\\')
 && !raw.includes('..')
 && (raw.startsWith('/chat') || raw.startsWith('/staff'))
 const target = isSafeRedirect ? raw : homeByRole(res.user.role)
 // 跨 MPA 跳转必须用 location.href（vue-router 无法跨 SPA）
 if (target.startsWith('/staff')) {
 window.location.href = target // ponytail:allow-location 跨 MPA 跳转
 } else {
 router.push(target)
 }
 } catch (e) {
 errorMsg.value = errmsg(e, '登录失败，请稍后重试')
 showFailToast(errorMsg.value)
 } finally {
 loading.value = false
 }
}

function goRegister() {
 router.push('/chat/register')
}

function goForgotPassword() {
 router.push('/chat/forgot-password')
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
 <!-- 品牌区 Hero -->
 <header class="flex flex-col items-center gap-[var(--spacer-4)] text-center">
 <BrandLogo size="md" />
 <p class="mt-[var(--spacer-8)] text-heading-sm font-semibold text-text tracking-[-0.01em]">
 智能健康宣教平台
 </p>
 <p class="text-body-sm text-text-secondary">
 7×24 小时 AI 健康问答 · 可溯源的健康指导
 </p>
 </header>

 <!-- 登录卡片 -->
 <section class="flex w-full flex-col gap-[var(--spacer-20)] rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] p-[var(--spacer-24)] border border-[var(--border-neutral-l1)] shadow-[var(--shadow-sm)]">
 <div>
 <h2 class="font-heading text-heading-lg font-semibold text-text">
 欢迎回来
 </h2>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-secondary">
 登录以继续您的健康管理
 </p>
 </div>

 <!-- 表单 -->
 <form class="flex flex-col gap-[var(--spacer-12)]" @submit.prevent="handleLogin">
 <div class="ds-field-wrap ds-field-wrap--secondary">
 <User class="h-4 w-4 shrink-0 text-icon-brand" />
 <input v-model="username" placeholder="请输入用户名或手机号" autocomplete="username" aria-label="用户名">
 </div>

 <DsPasswordField v-model="password" placeholder="请输入密码" autocomplete="current-password" aria-label="密码" tone="brand" />

 <div class="flex justify-end -mt-[var(--spacer-4)]">
<button
type="button"
class="ds-link-btn"
@click="goForgotPassword"
>
忘记密码？
</button>
</div>

 <div
 v-if="errorMsg"
 class="ds-alert ds-alert--error"
 role="alert"
 >
 <CircleAlert class="icon" />
 <span>{{ errorMsg }}</span>
 </div>

 <DsSubmitButton :loading="loading" text="登录" />
 </form>
 </section>

 <!-- 底部链接区 -->
 <footer class="flex flex-col items-center gap-[var(--spacer-12)] text-center">
 <p class="text-body-md text-text-tertiary">
 还没有账号？
 <button
 class="ds-link-btn font-medium"
 @click="goRegister"
>立即注册</button>
 </p>
 <button
 class="ds-link-btn"
 @click="router.push('/chat')"
 >
<MessageCircle :size="16" class="shrink-0" />
暂不登录，直接提问
</button>
 </footer>
 </div>
 </PageShell>
</template>
