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
        :aria-label="isLoggedIn ? '历史记录' : '登录'"
        @click="isLoggedIn ? emit('openHistory') : goProfileOrLogin()"
      >
        <!-- 未登录：空心用户；登录：实心用户剪影（fill 受控） -->
        <User :size="18" :fill="isLoggedIn ? 'currentColor' : 'none'" />
      </button>
    </template>
  </AppHeader>
</template>
