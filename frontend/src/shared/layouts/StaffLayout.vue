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
    label: '对话测试',
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

/** 自带 AppHeader 返回按钮的二级页面，隐藏 tabbar 提升沉浸感 */
const hideOnRoutes = [
  'staff-article-create',
  'staff-article-edit',
  'staff-crisis-events',
  'staff-references',
  'staff-config-home',
  'staff-config-accounts',
  'staff-config-account-detail',
  'staff-config-ai-providers',
  'staff-config-ai-provider-create',
  'staff-config-ai-provider-edit',
  'staff-config-sensitive-words',
  'staff-config-safety-rules',
  'staff-config-rag',
  'staff-config-safety-messages',
  'staff-config-prompts',
  'staff-config-prompt-create',
  'staff-config-prompt-detail',
  'staff-config-audit-logs',
  'staff-config-departments',
]
</script>

<template>
  <BottomNavLayout :items="navItems" :default-active="defaultActive" :hide-on-routes="hideOnRoutes" />
</template>
