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
      <!-- 历史记录 / 登录：统一品牌色图标按钮，登录后点击打开历史抽屉 -->
      <button
        class="ds-icon-btn--brand"
        :class="isLoggedIn ? 'header-user-btn--logged' : ''"
        :aria-label="isLoggedIn ? '历史记录' : '登录'"
        @click="isLoggedIn ? emit('openHistory') : goProfileOrLogin()"
      >
        <User :size="18" />
      </button>
    </template>
  </AppHeader>
</template>

<style scoped ponytail:allow-scoped-css 组件级样式覆盖，折中>
/* 登录后：浅品牌底 + 黄色图标前景，与未登录的品牌实底区分 */
.header-user-btn--logged {
  background: var(--bg-brand-light);
  color: var(--text-brand);
}
</style>
