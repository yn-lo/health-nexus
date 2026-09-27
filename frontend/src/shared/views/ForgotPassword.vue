<script setup lang="ts">
/**
 * ForgotPassword 忘记密码页 — 管理员协助流程
 *
 * 平台不提供自助找回密码：重置令牌没有可靠的投递渠道（无短信/邮件），
 * 仅写入后端 Redis 会让患者卡在无法获取令牌的步骤上（P2）。
 * 因此本页只做引导：由医院信息科 / 服务窗口核验身份后由管理员重置密码。
 * 管理员重置通道见 `resetStaffAccountPassword`（POST /api/staff/auth/accounts/{id}/reset-password）。
 */
import { ShieldCheck } from '@lucide/vue'
import { PageShell, AuthHero } from '@/shared/components'

function goLogin() {
  // 统一登录页在 /login（chat SPA），需跨 MPA 跳转
  window.location.href = '/login' // ponytail:allow-location 跨 MPA 跳转
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
      <!-- 上半屏：品牌叙事区 -->
      <AuthHero>
        <template #title>找回密码</template>
        <template #subtitle>密码重置需由医院信息科协助完成</template>
      </AuthHero>

      <!-- 下半屏：协助说明 -->
      <section class="auth-form-area flex flex-1 flex-col justify-center gap-[var(--spacer-16)] pb-[var(--spacer-16)]">
        <div class="ds-alert ds-alert--warning">
          <ShieldCheck class="icon" />
          <p class="text-body-sm">
            为保障账户安全，本平台不提供自助重置密码，请通过以下方式联系我们：
          </p>
        </div>

        <ol class="flex flex-col gap-[var(--spacer-12)] text-body-base text-text-secondary">
          <li>前往医院信息科现场办理；</li>
          <li>携带本人有效证件至服务窗口核验身份；</li>
          <li>由管理员重置后，使用新密码重新登录。</li>
        </ol>

        <button
          class="auth-submit-btn ds-btn ds-btn--primary ds-btn--block"
          @click="goLogin"
        >
          返回登录
        </button>
      </section>
    </div>
  </PageShell>
</template>
