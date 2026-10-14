<script setup lang="ts">
/**
 * PersonalCenter 个人中心
 * ProfileHeader + 资料摘要 + 菜单(我的对话/编辑资料/修改密码/关于我们) + 退出登录
 * 底部导航由 ChatLayout 提供
 */
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { MessageCircle, Info, KeyRound, Pencil } from '@lucide/vue'
import { useAuthStore } from '@/stores/auth'
import { ProfileHeader, MenuList, MenuRow, ProfileSummaryCard } from '@/shared/components'
import { useProfileSummary } from '@/shared'
import type { MenuItem } from '@/shared'
import { fmtUserId } from '@/shared'

const router = useRouter()
const authStore = useAuthStore()

const { profileSummary } = useProfileSummary()

const userName = computed(() => authStore.user?.username ?? '用户')
const firstChar = computed(() => userName.value.charAt(0).toUpperCase())

const patientId = computed(() => `ID: ${fmtUserId(authStore.user?.id ?? 0)}`)
const metaLines = computed(() => [patientId.value])

const menuItems: MenuItem[] = [
  { icon: MessageCircle, label: '我的对话', routeName: 'chat-home' },
  { icon: Pencil, label: '编辑资料', routeName: 'chat-edit-profile' },
  { icon: KeyRound, label: '修改密码', routeName: 'chat-change-password' },
  { icon: Info, label: '关于我们', routeName: 'about-us' },
]

onMounted(() => {
  authStore.fetchProfile()
})

function goMenu(routeName: string) {
  router.push({ name: routeName })
}

async function handleLogout() {
  await authStore.logout()
  // ponytail:allow-location 跨 MPA 跳转（chat → 登录页），非 SPA 内部路由
  window.location.href = '/login'
}
</script>

<template>
 <div class="min-h-screen bg-[var(--bg-base-secondary)]">
  <!-- 个人头部 -->
  <ProfileHeader :name="userName" :avatar-text="firstChar" :meta="metaLines" />

  <!-- 资料摘要 -->
  <ProfileSummaryCard :lines="profileSummary" />

  <!-- 菜单列表 -->
  <section class="px-[var(--spacer-16)] pb-[var(--spacer-16)]">
   <MenuList>
    <MenuRow
     v-for="item in menuItems"
     :key="item.label"
     :icon="item.icon"
     :label="item.label"
     @click="goMenu(item.routeName)"
    />
   </MenuList>
  </section>

 <!-- 退出登录按钮（危险操作，使用 status-error 令牌） -->
 <section class="px-[var(--spacer-16)] pt-[var(--spacer-8)] pb-[var(--spacer-16)]">
 <button
 class="w-full min-h-[var(--touch-target-min)] rounded-[var(--radius-8)] flex items-center justify-center border border-[var(--status-error-default)] bg-transparent text-[var(--status-error-default)] hover:bg-[var(--status-error-surface-l1)] active:bg-[var(--status-error-surface-l1)] focus-visible:shadow-[var(--focus-ring)] transition-[background-color_var(--micro-duration)_var(--micro-ease)] text-body-base-strong font-medium"
 @click="handleLogout"
 >
 退出登录
 </button>
 </section>
 </div>
</template>
