<script setup lang="ts">
/**
 * 医护端工作台首页 — 三层分化
 * 根据角色动态展示：快捷操作（全员）/ 科室管理（管理员）/ 系统管理（超管）
 * 底部导航由 StaffLayout 提供，本视图不渲染
 */
import { ref, computed, onMounted } from 'vue'
import type { Component } from 'vue'
import { useRouter } from 'vue-router'
import {
  AlertTriangle,
  Building2,
  Cpu,
  FileText,
  MessageSquareWarning,
  ScrollText,
  Share2,
  ShieldAlert,
  ShieldBan,
  SlidersHorizontal,
  Users,
} from '@lucide/vue'
import {
 SectionHeading,
 StatRow,
 QuickActionGrid,
 QuickActionItem,
 EmptyState,
 BrandLogo,
} from '@/shared/components'
import { staffChatApi, wikiApi } from '@/shared'
import { useAuthStore } from '@/stores/auth'
import { ADMIN_ROLES, SUPER_ADMIN_ROLE } from '@/shared/constants/roles'

const router = useRouter()
const authStore = useAuthStore()

const isAdmin = computed(() => {
 const role = authStore.user?.role
 return role ? (ADMIN_ROLES as readonly string[]).includes(role) : false
})

const isSuperAdmin = computed(() => authStore.user?.role === SUPER_ADMIN_ROLE)

/** 三个统计指标 */
const crisisUnhandled = ref(0)
const pendingArticles = ref(0)
const draftArticles = ref(0)
const loaded = ref(false)

/** 用户名 */
const userName = computed(() => authStore.user?.username ?? '医生')

/** 当前问候语 */
const greeting = computed(() => {
 const hour = new Date().getHours()
 if (hour < 12) return '早上好'
 if (hour < 18) return '下午好'
 return '晚上好'
})

/** 格式化日期 */
const todayDate = computed(() => {
 const d = new Date()
 const weekdays = ['日', '一', '二', '三', '四', '五', '六']
 return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日 星期${weekdays[d.getDay()]}`
})

/** StatRow 数据 */
const statItems = computed(() => [
 { value: crisisUnhandled.value, label: '未处理危机' },
 { value: pendingArticles.value, label: '待审文章' },
 { value: draftArticles.value, label: '草稿' },
])

interface QuickAction {
 icon: Component
 label: string
 routeName: string
 badge?: number
}

/** 快捷操作 — 所有医护角色可见 */
const quickActions = computed<QuickAction[]>(() => [
  {
    icon: ShieldAlert,
    label: '危机事件',
    routeName: 'staff-crisis-events',
    badge: crisisUnhandled.value > 0 ? crisisUnhandled.value : undefined,
  },
  {
    icon: FileText,
    label: '文章管理',
    routeName: 'staff-articles',
  },
  {
    icon: Share2,
    label: '跨科室引用',
    routeName: 'staff-references',
  },
])

/** 科室管理 — 科室管理员 + 超管可见 */
const deptActions = computed<QuickAction[]>(() => [
 {
  icon: Users,
  label: '人员管理',
  routeName: 'staff-config-accounts',
 },
 {
  icon: FileText,
  label: '提示词模板',
  routeName: 'staff-config-prompts',
 },
])

/** 系统管理 — 仅超管可见 */
const systemActions = computed<QuickAction[]>(() => [
 {
  icon: Building2,
  label: '科室设置',
  routeName: 'staff-config-departments',
 },
 {
  icon: Cpu,
  label: 'AI 提供商',
  routeName: 'staff-config-ai-providers',
 },
 {
 icon: SlidersHorizontal,
 label: 'RAG 参数',
 routeName: 'staff-config-rag',
 },
 {
 icon: ShieldBan,
 label: '敏感词库',
 routeName: 'staff-config-sensitive-words',
 },
 {
 icon: AlertTriangle,
 label: '安全规则',
 routeName: 'staff-config-safety-rules',
 },
 {
 icon: MessageSquareWarning,
 label: '安全话术',
 routeName: 'staff-config-safety-messages',
 },
 {
 icon: ScrollText,
 label: '审计日志',
 routeName: 'staff-config-audit-logs',
 },
])

/** 通知红点总数 */
const totalBadge = computed(() => crisisUnhandled.value + pendingArticles.value)

function goRoute(name: string) {
 if (name.startsWith('/')) {
 // ponytail:allow-location — 跨 MPA 跳转，折中
 window.location.href = name
 return
 }
 router.push({ name })
}

onMounted(async () => {
 try {
 const [crisisRes, pendingRes, draftRes] = await Promise.all([
 staffChatApi.listCrisisEvents({ handled: false, page: 1, page_size: 1 }),
 wikiApi.listMyArticles({ status: 'pending', page: 1, page_size: 1 }),
 wikiApi.listMyArticles({ status: 'draft', page: 1, page_size: 1 }),
 ])
 crisisUnhandled.value = crisisRes.total
 pendingArticles.value = pendingRes.total
 draftArticles.value = draftRes.total
 } catch {
 // 保留空状态展示，不阻塞页面
 } finally {
 loaded.value = true
 }
})
</script>

<template>
 <div class="bg-[var(--bg-base-default)] min-h-screen min-h-dvh">
 <!-- Greeting Header + Quick Stats -->
<section class="px-[var(--spacer-16)] pt-[var(--spacer-24)] pb-[var(--spacer-16)]">
<div class="mb-[var(--spacer-12)] flex items-center gap-[var(--spacer-16)]">
<div class="dashboard-logo shrink-0">
<BrandLogo size="sm" orientation="horizontal" hide-name />
</div>
<div class="min-w-0">
<p class="text-body-sm text-text-tertiary">{{ todayDate }}</p>
<h1 class="font-heading text-heading-lg font-semibold leading-tight">
<span class="text-text-brand">{{ greeting }}</span>
<span class="text-text">，{{ userName }}</span>
</h1>
</div>
</div>
 <div class="ds-card p-[var(--spacer-16)]">
 <StatRow :stats="statItems" />
 </div>
 </section>

 <!-- 快捷操作 — 全员 -->
 <section class="px-[var(--spacer-16)] pb-[var(--spacer-20)]">
 <SectionHeading text="快捷操作" />
 <QuickActionGrid>
 <QuickActionItem
 v-for="action in quickActions"
 :key="action.label"
 :icon="action.icon"
 :label="action.label"
 :badge="action.badge"
 @click="goRoute(action.routeName)"
 />
 </QuickActionGrid>
 </section>

 <!-- 科室管理 — 管理员 + 超管 -->
 <section v-if="isAdmin" class="px-[var(--spacer-16)] pb-[var(--spacer-20)]">
 <SectionHeading text="科室管理" />
 <QuickActionGrid>
 <QuickActionItem
 v-for="action in deptActions"
 :key="action.label"
 :icon="action.icon"
 :label="action.label"
 @click="goRoute(action.routeName)"
 />
 </QuickActionGrid>
 </section>

 <!-- 系统管理 — 仅超管 -->
 <section v-if="isSuperAdmin" class="px-[var(--spacer-16)] pb-[var(--spacer-24)]">
 <SectionHeading text="系统管理" />
 <QuickActionGrid>
 <QuickActionItem
 v-for="action in systemActions"
 :key="action.label"
 :icon="action.icon"
 :label="action.label"
 @click="goRoute(action.routeName)"
 />
 </QuickActionGrid>
 </section>

 <!-- 空状态 -->
 <section v-if="loaded && totalBadge === 0 && draftArticles === 0" class="px-[var(--spacer-16)] pb-[var(--spacer-32)]">
 <div class="rounded-[var(--radius-12)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] px-[var(--spacer-16)] py-[var(--spacer-32)] text-center">
 <EmptyState text="暂无待处理事项" />
 </div>
 </section>
 </div>
</template>

<style scoped ponytail:allow-scoped-css 页面专属样式，折中>
/* 本页面 Logo 放大 + 品牌光晕，不影响全局 BrandLogo */
.dashboard-logo :deep(.brand-logo__icon-wrapper) {
  width: 48px;  /* style-guard:ignore dashboard-only logo */
  height: 48px; /* style-guard:ignore dashboard-only logo */
  box-shadow: var(--shadow-glow-sm);
}
</style>
