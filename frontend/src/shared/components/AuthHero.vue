<script setup lang="ts">
/**
 * AuthHero — 认证页上半屏品牌叙事区（Login/Register/ForgotPassword 共用）
 * 品牌行 + 大标题 + 副标题，标题/副标题通过 slot 传入以支持动态内容
 */
import { BrandLogo } from '@/shared/components'
</script>

<template>
  <header class="auth-hero flex flex-col justify-end pb-[var(--spacer-24)]">
    <div class="auth-brand-row flex items-center gap-[var(--spacer-10)]">
      <BrandLogo size="sm" hide-name />
      <span class="auth-brand-name font-heading text-body-sm-strong tracking-[0.14em] text-text-tertiary">
        HEALTH NEXUS
      </span>
    </div>

    <div class="mt-[var(--spacer-28)] flex flex-col gap-[var(--spacer-12)]">
      <h1 class="auth-title font-heading font-semibold leading-[1.15] tracking-[-0.02em] text-text">
        <slot name="title" />
      </h1>
      <p class="auth-subtitle text-body-base leading-relaxed text-text-secondary">
        <slot name="subtitle" />
      </p>
    </div>
  </header>
</template>
