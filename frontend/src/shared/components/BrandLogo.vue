<script setup lang="ts">
import logo from '@/assets/images/hn_logo.png'

withDefaults(defineProps<{
  size?: 'sm' | 'md' | 'lg'
  orientation?: 'vertical' | 'horizontal'
  /** 隐藏品牌文字，仅显示图标 */
  hideName?: boolean
}>(), {
  size: 'md',
  orientation: 'vertical',
  hideName: false,
})
</script>

<template>
  <div
    class="brand-logo"
    :class="[
      orientation === 'vertical' ? 'flex-col' : 'flex-row items-center',
      `brand-logo--${size}`,
    ]"
  >
    <div class="brand-logo__icon-wrapper">
      <img
        :src="logo"
        alt="Health Nexus Logo"
        class="brand-logo__img"
      >
    </div>
    <span v-if="!hideName" class="brand-logo__name">Health Nexus</span>
  </div>
</template>

<style scoped ponytail:allow-scoped-css 组件级样式覆盖，折中>
.brand-logo {
  display: flex;
  align-items: center;
}

.brand-logo--sm {
  gap: var(--spacer-8);
}
.brand-logo--sm .brand-logo__icon-wrapper {
  width: 35px; /* style-guard:ignore logo size */
  height: 35px; /* style-guard:ignore logo size */
}
.brand-logo--sm .brand-logo__name {
  font-size: var(--heading-sm-font-size);
  line-height: var(--heading-sm-line-height);
}

.brand-logo--md {
  gap: var(--spacer-12);
}
.brand-logo--md .brand-logo__icon-wrapper {
  width: 64px;
  height: 64px;
}
.brand-logo--md .brand-logo__name {
  font-size: var(--heading-lg-font-size);
  line-height: var(--heading-lg-line-height);
}

.brand-logo--lg {
  gap: var(--spacer-16);
}
.brand-logo--lg .brand-logo__icon-wrapper {
  width: 80px;
  height: 80px;
}
.brand-logo--lg .brand-logo__name {
  font-size: var(--heading-xl-font-size);
  line-height: var(--heading-xl-line-height);
}

/* Logo 图标容器 - 与 header-action-btn 统一圆形悬浮样式 */
.brand-logo__icon-wrapper {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  border-radius: var(--radius-full);
  overflow: hidden;
  background: var(--bg-base-default);
  box-shadow: var(--shadow-sm);
  transition: transform var(--micro-duration) var(--micro-ease),
              box-shadow var(--micro-duration) var(--micro-ease);
}

.brand-logo__icon-wrapper:hover {
  transform: scale(var(--hover-scale));
  box-shadow: var(--shadow-md);
}

.brand-logo__icon-wrapper:active {
  transform: scale(var(--press-scale));
}

.brand-logo__img {
  width: 70%;
  height: 70%;
  object-fit: contain;
  margin: auto;
}

/* 品牌文字 — AI 渐变 */
.brand-logo__name {
  font-family: var(--font-heading);
  font-weight: var(--font-weight-strong);
  background: linear-gradient(135deg, var(--bg-brand-active) 0%, var(--ai-accent) 40%, var(--bg-brand-hover) 100%); /* style-guard:ignore decorative gradient */
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  letter-spacing: -0.02em;
}

/* A11y：减弱动效 */
@media (prefers-reduced-motion: reduce) {
  .brand-logo__icon-wrapper,
  .brand-logo__icon-wrapper:hover,
  .brand-logo__icon-wrapper:active {
    transition: none;
    transform: none;
  }
}
</style>
