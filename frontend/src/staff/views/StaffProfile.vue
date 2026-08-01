<script setup lang="ts">
/**
 * 医护端个人中心
 * 浅灰背景 + 白色卡片分组，对齐现代移动端设置页风格
 * ProfileHeader（含角色标签）+ 资料摘要 + 分组菜单 + 退出登录卡片
 */
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Info, LogOut, KeyRound, Pencil } from '@lucide/vue'
import { useAuthStore } from '@/stores/auth'
import { ProfileHeader, MenuList, MenuRow } from '@/shared/components'
import { ROLE_LABEL, DEFAULT_STAFF_LABEL, type UserRole } from '@/shared/constants/roles'
import { useProfileSummary } from '@/shared'
import type { MenuItem } from '@/shared'
import { fmtUserId } from '@/shared'

const router = useRouter()
const authStore = useAuthStore()

const { profileSummary } = useProfileSummary()

const user = computed(() => authStore.user)

const userName = computed(() => user.value?.username ?? '医生')
const firstChar = computed(() => userName.value.charAt(0).toUpperCase())

const titleText = computed(() => {
  const role = user.value?.role as UserRole | undefined
  return role ? (ROLE_LABEL[role] ?? DEFAULT_STAFF_LABEL) : DEFAULT_STAFF_LABEL
})

const staffId = computed(
  () => `工号: ${fmtUserId(authStore.user?.id ?? 0)}`,
)

const metaLines = computed(() => [staffId.value])

const menuItems: MenuItem[] = [
  { icon: Pencil, label: '编辑资料', routeName: 'staff-edit-profile' },
  { icon: KeyRound, label: '修改密码', routeName: 'staff-change-password' },
  { icon: Info, label: '关于我们', routeName: 'about-us' },
]

onMounted(() => {
  authStore.fetchProfile()
})

function goMenu(routeName: string) {
  if (routeName === 'about-us') {
    // ponytail:allow-location — 跨 MPA 跳转，折中
    window.location.href = '/about'
    return
  }
  router.push({ name: routeName })
}

async function handleLogout() {
  await authStore.logout()
  window.location.href = '/login' // ponytail:allow-location 跨 MPA 跳转
}
</script>

<template>
  <div class="min-h-screen bg-[var(--bg-base-secondary)]">
    <!-- 个人头部 -->
    <ProfileHeader :name="userName" :avatar-text="firstChar" :meta="metaLines" :badge="titleText" />

    <!-- 资料摘要 -->
    <div
      v-if="profileSummary.length"
      class="mx-[var(--spacer-16)] mb-[var(--spacer-16)] rounded-[var(--radius-12)] bg-[var(--bg-base-default)] px-[var(--spacer-16)] py-[var(--spacer-12)] border border-[var(--border-neutral-l1)]"
    >
      <div class="flex flex-col gap-[var(--spacer-4)]">
        <div
          v-for="line in profileSummary"
          :key="line.text"
          class="flex items-center gap-[var(--spacer-8)] text-body-sm text-text-secondary"
        >
          <component :is="line.icon" class="h-4 w-4 shrink-0 text-icon-tertiary" />
          <span>{{ line.text }}</span>
        </div>
      </div>
    </div>

    <!-- 功能菜单 -->
    <div class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
      <MenuList>
        <MenuRow
          v-for="item in menuItems"
          :key="item.routeName"
          :icon="item.icon"
          :label="item.label"
          @click="goMenu(item.routeName)"
        />
      </MenuList>
    </div>

    <!-- 退出登录 -->
    <div class="px-[var(--spacer-16)] pb-[var(--spacer-32)]">
      <MenuList>
        <MenuRow :icon="LogOut" label="退出登录" danger @click="handleLogout" />
      </MenuList>
    </div>
  </div>
</template>