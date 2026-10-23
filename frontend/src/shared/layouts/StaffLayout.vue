<script setup lang="ts">
import { computed } from 'vue'
import { LayoutDashboard, MessageCircle, User } from '@lucide/vue'
import BottomNavLayout from './BottomNavLayout.vue'
import { useAuthStore } from '@/stores/auth'
import { ADMIN_ROLES } from '@/shared/constants/roles'

const auth = useAuthStore()

const isAdmin = computed(() => {
  const role = auth.user?.role
  return role ? (ADMIN_ROLES as readonly string[]).includes(role) : false
})

/** 医护端底部导航布局
 * 管理员：对话测试 / 工作台 / 我的
 * 普通医护：对话测试 / 我的 */
const allNavItems = [
  {
    key: 'chat',
    label: '对话',
    iconComponent: MessageCircle,
    routeNames: [],
    routeName: '',
    externalUrl: '/chat',
    adminOnly: false,
  },
  {
    key: 'dashboard',
    label: '工作台',
    iconComponent: LayoutDashboard,
    routeNames: ['staff-dashboard'],
    routeName: 'staff-dashboard',
    adminOnly: true,
  },
  {
    key: 'profile',
    label: '我的',
    iconComponent: User,
    routeNames: ['staff-profile'],
    routeName: 'staff-profile',
    adminOnly: false,
  },
]

const navItems = computed(() =>
  allNavItems.filter((item) => !item.adminOnly || isAdmin.value),
)

const defaultActive = computed(() => (isAdmin.value ? 'dashboard' : 'profile'))

</script>

<template>
  <BottomNavLayout :items="navItems" :default-active="defaultActive" />
</template>
