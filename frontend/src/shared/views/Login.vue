<script setup lang="ts">
/**
 * Login 统一登录页 - 「极简编辑风」上下分屏设计
 *
 * 单一登录入口，不区分患者/医护。登录成功后按 user.role 自动跳转：
 * - PATIENT -> /chat
 * - SUPER_ADMIN/DEPT_ADMIN/DOCTOR/NURSE -> /staff
 *
 * 设计方向（全新语言，与旧版无关）：
 * - 上下分屏：上半屏品牌叙事（大标题 + 价值主张），下半屏登录表单
 * - 极简留白编辑风：大量留白、大字号、克制配色，靠排版营造高级感
 * - 无卡片容器：表单直接铺在留白背景上，摆脱「表单卡片」套路
 * - 下划线式输入框：更极简，聚焦时品牌强调色
 * - 大字号 + 清晰层级：老年患者可读性优先
 *
 * 与 Register / ForgotPassword / ChangePassword 共用 PageShell + 设计令牌，
 * 不写组件级 scoped 样式（遵循 frontend/CLAUDE.md 硬性规则 1）。
 */
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { CircleAlert, MessageCircle } from '@lucide/vue'
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
    :padded="false"
    background="var(--auth-bg)"
    class="auth-page relative overflow-hidden"
  >
    <!-- 背景装饰：极简光晕 -->
    <div class="auth-aura auth-aura--top" aria-hidden="true" />

    <div class="auth-scroll relative mx-auto flex min-h-dvh w-full max-w-[400px] flex-col px-[var(--spacer-24)]">
      <!-- 上半屏：品牌叙事区（视觉重心） -->
      <header class="auth-hero flex flex-col justify-end pb-[var(--spacer-24)]">
        <div class="auth-brand-row flex items-center gap-[var(--spacer-10)]">
          <BrandLogo size="sm" hide-name />
          <span class="auth-brand-name font-heading text-body-sm-strong tracking-[0.14em] text-text-tertiary">
            HEALTH NEXUS
          </span>
        </div>

        <div class="mt-[var(--spacer-28)] flex flex-col gap-[var(--spacer-12)]">
          <h1 class="auth-title font-heading font-semibold leading-[1.15] tracking-[-0.02em] text-text">
            智能健康<br>宣教平台
          </h1>
          <p class="auth-subtitle text-body-base leading-relaxed text-text-secondary">
            7×24 小时 AI 健康问答，<br>为患者提供可溯源的健康指导
          </p>
        </div>
      </header>

      <!-- 下半屏：登录表单区（功能） -->
      <section class="auth-form-area flex flex-1 flex-col justify-center pb-[var(--spacer-16)]">
        <form class="auth-form flex flex-col gap-[var(--spacer-16)]" @submit.prevent="handleLogin">
          <div class="auth-field">
            <label class="auth-label" for="auth-username">用户名</label>
            <div class="ds-field-wrap ds-field-wrap--underline">
              <input
                id="auth-username"
                v-model="username"
                placeholder="请输入用户名或手机号"
                autocomplete="username"
                aria-label="用户名"
              >
            </div>
          </div>

          <div class="auth-field">
            <label class="auth-label" for="auth-password">密码</label>
            <DsPasswordField
              v-model="password"
              placeholder="请输入密码"
              autocomplete="current-password"
              aria-label="密码"
              tone="brand"
            />
          </div>

          <div class="flex justify-end -mt-[var(--spacer-4)]">
            <button type="button" class="ds-link-btn" @click="goForgotPassword">
              忘记密码？
            </button>
          </div>

          <div v-if="errorMsg" class="ds-alert ds-alert--error" role="alert">
            <CircleAlert class="icon" />
            <span>{{ errorMsg }}</span>
          </div>

          <DsSubmitButton :loading="loading" text="登 录" class="auth-submit-btn" />
        </form>

        <!-- 底部链接区 -->
        <footer class="auth-footer mt-[var(--spacer-24)] flex flex-col items-center gap-[var(--spacer-12)] text-center">
          <p class="text-body-base text-text-secondary">
            还没有账号？
            <button class="ds-link-btn font-medium" @click="goRegister">立即注册</button>
          </p>
          <button class="auth-guest-btn" @click="router.push('/chat')">
            <MessageCircle :size="16" class="shrink-0" />
            暂不登录，直接提问
          </button>
        </footer>
      </section>
    </div>
  </PageShell>
</template>
