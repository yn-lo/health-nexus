<script setup lang="ts">
/**
 * ChatHeader - 患者端统一头部
 *
 * 复用于 ChatHome 和 ChatConversation，保持 header 样式一致。
 * - 左侧：始终显示 BrandLogo + 可选返回按钮
 * - 中间：slot 传入内容（胶囊切换等）
 * - 右侧：历史记录 / 登录按钮，样式与 header-action-btn 一致
 */
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { User } from '@lucide/vue'
import { AppHeader, BrandLogo } from '@/shared/components'
import { useAuthStore } from '@/stores/auth'
import { STAFF_ROLES, type UserRole } from '@/shared/constants/roles'

const props = withDefaults(defineProps<{
  variant?: 'solid' | 'frosted' | 'transparent'
}>(), {
  variant: 'transparent',
})

const emit = defineEmits<{
  openHistory: []
}>()

const router = useRouter()
const authStore = useAuthStore()

const isLoggedIn = computed(() => !!authStore.user)
const userInitial = computed(() => (authStore.user?.username ?? '?').charAt(0).toUpperCase())

function goProfileOrLogin() {
  if (isLoggedIn.value) {
    const role = authStore.user?.role as UserRole
    if (role && STAFF_ROLES.includes(role)) {
      window.location.href = '/staff/profile' // ponytail:allow-location 跨 MPA 跳转
    } else {
      router.push({ name: 'personal-center' })
    }
  } else {
    router.push({ name: 'login' })
  }
}

function onLogoClick() {
  router.push({ name: 'about-us' })
}
</script>

<template>
  <AppHeader :variant="props.variant" :show-back="false">
    <template #left>
      <button type="button" aria-label="关于我们" @click="onLogoClick" class="block">
        <BrandLogo size="sm" orientation="horizontal" hide-name />
      </button>
    </template>
    <template #center>
      <slot name="center" />
    </template>
    <template #right>
      <!-- 历史记录 -->
      <button
        v-if="isLoggedIn"
        class="header-action-btn ds-avatar ds-avatar--brand text-body-sm-strong font-semibold"
        aria-label="历史记录"
        @click="emit('openHistory')"
      >
        <span>{{ userInitial }}</span>
      </button>
      <button
        v-else
        class="ds-icon-btn--brand"
        aria-label="登录"
        @click="goProfileOrLogin"
      >
        <User :size="18" />
      </button>
    </template>
  </AppHeader>
</template>

<style scoped ponytail:allow-scoped-css 组件级样式覆盖，折中>
/* ── Header 操作按钮：统一 44px 圆形 + 阴影悬浮（WCAG 最小触摸目标） ──── */
.header-action-btn {
  width: 44px;
  height: 44px;
  flex-shrink: 0;
  border: none;
  cursor: pointer;
  box-shadow: var(--shadow-sm);
  transition: transform var(--micro-duration) var(--micro-ease),
              box-shadow var(--micro-duration) var(--micro-ease);
}
.header-action-btn:hover {
  transform: scale(var(--hover-scale));
  box-shadow: var(--shadow-md);
}
.header-action-btn:active {
  transform: scale(var(--press-scale));
}
</style>
