<script setup lang="ts">
/**
 * 医护端工作台首页 — 卡片式重构
 * 参照百度翻译个人页面风格：顶部问候 + 统计行 → 快捷操作深色卡 → 推荐功能白卡 → 更多服务白卡
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
  Sparkles,
  Users,
} from '@lucide/vue'
import {
 SectionHeading,
 StatRow,
 QuickActionGrid,
 QuickActionItem,
 BrandLogo,
} from '@/shared/components'
import { staffChatApi, wikiApi } from '@/shared'
import { useAuthStore } from '@/stores/auth'
import { ADMIN_ROLES, SUPER_ADMIN_ROLE, type UserRole } from '@/shared/constants/roles'

const router = useRouter()
const authStore = useAuthStore()

/** 三个统计指标 */
const crisisUnhandled = ref(0)
const pendingArticles = ref(0)
const draftArticles = ref(0)

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
 return `${d.getMonth() + 1}月${d.getDate()}日 星期${weekdays[d.getDay()]}`
})

/** StatRow 数据 */
const statItems = computed(() => [
 { value: crisisUnhandled.value, label: '未处理危机' },
 { value: pendingArticles.value, label: '待审文章' },
 { value: draftArticles.value, label: '草稿' },
])

/** 权限层级：staff=全体医护 / admin=管理员 / super=仅超管 */
type ActionLevel = 'staff' | 'admin' | 'super'

interface QuickAction {
 icon: Component
 label: string
 routeName: string
 badge?: number
 level: ActionLevel
}

/** 卡片级权限判断（与各路由守卫对齐，见 staff/router/index.ts） */
function canSee(level: ActionLevel): boolean {
 const role = authStore.user?.role
 if (level === 'super') return role === SUPER_ADMIN_ROLE
 if (level === 'admin') return role ? (ADMIN_ROLES as UserRole[]).includes(role as UserRole) : false
 return true // staff：能进入医护端即默认可见
}

/** 科室管理 — 文章/引用(全体医护) + 人员(管理员) */
const deptActions = computed<QuickAction[]>(() => [
 { icon: FileText, label: '文章管理', routeName: 'staff-articles', level: 'staff' },
 { icon: Share2, label: '引用管理', routeName: 'staff-references', level: 'staff' },
 { icon: Users, label: '人员管理', routeName: 'staff-config-accounts', level: 'admin' },
])
const deptVisible = computed(() => deptActions.value.filter(a => canSee(a.level)))
const systemVisible = computed(() => systemActions.value.filter(a => canSee(a.level)))

/** 系统管理 — 全局/系统级配置；危机与提示词因非科室层面归入此处 */
const systemActions = computed<QuickAction[]>(() => [
 {
  icon: ShieldAlert,
  label: '危机事件',
  routeName: 'staff-crisis-events',
  badge: crisisUnhandled.value > 0 ? crisisUnhandled.value : undefined,
  level: 'staff',
 },
 {
  icon: Sparkles,
  label: '提示词模板',
  routeName: 'staff-config-prompts',
  level: 'admin',
 },
 {
  icon: SlidersHorizontal,
  label: 'RAG 参数',
  routeName: 'staff-config-rag',
  level: 'admin',
 },
 {
  icon: Building2,
  label: '科室设置',
  routeName: 'staff-config-departments',
  level: 'super',
 },
 {
  icon: Cpu,
  label: 'AI 提供商',
  routeName: 'staff-config-ai-providers',
  level: 'super',
 },
 {
  icon: ShieldBan,
  label: '敏感词库',
  routeName: 'staff-config-sensitive-words',
  level: 'super',
 },
 {
  icon: AlertTriangle,
  label: '安全规则',
  routeName: 'staff-config-safety-rules',
  level: 'super',
 },
 {
  icon: MessageSquareWarning,
  label: '安全话术',
  routeName: 'staff-config-safety-messages',
  level: 'super',
 },
 {
  icon: ScrollText,
  label: '审计日志',
  routeName: 'staff-config-audit-logs',
  level: 'super',
 },
])

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
  // 统计失败时不阻塞页面，保留空值
 }
})
</script>

<template>
 <div class="bg-[var(--bg-base-default)] min-h-screen min-h-dvh">
  <!-- ══ 顶部问候区 ═══ -->
  <section class="dashboard-greeting px-[var(--spacer-16)] pt-[var(--spacer-24)] pb-[var(--spacer-16)]">
   <div class="flex items-center gap-[var(--spacer-12)] mb-[var(--spacer-16)]">
    <div class="dashboard-avatar shrink-0">
     <BrandLogo size="sm" orientation="horizontal" hide-name />
    </div>
    <div class="min-w-0 flex-1">
     <h1 class="font-heading text-heading-lg font-semibold leading-tight text-text-default">
      <span class="text-text-brand">{{ greeting }}</span>，{{ userName }}
     </h1>
     <p class="text-body-sm text-text-tertiary mt-[var(--spacer-2)]">{{ todayDate }}</p>
    </div>
   </div>

   <!-- 统计数字行（无卡片样式，纯数字，上下加大留白） -->
   <div class="py-[var(--spacer-8)]">
    <StatRow :stats="statItems" />
   </div>
  </section>

  <!-- ═══ 科室管理 — 白卡 ═══ -->
  <section v-if="deptVisible.length" class="dashboard-section px-[var(--spacer-16)] pb-[var(--spacer-20)]">
   <div class="ds-card p-[var(--spacer-16)]">
    <SectionHeading text="科室管理" />
    <QuickActionGrid>
     <QuickActionItem
      v-for="action in deptVisible"
      :key="action.label"
      :icon="action.icon"
      :label="action.label"
      @click="goRoute(action.routeName)"
     />
    </QuickActionGrid>
   </div>
  </section>

  <!-- ═══ 系统管理 — 白卡 ═══ -->
  <section v-if="systemVisible.length" class="dashboard-section px-[var(--spacer-16)] pb-[var(--spacer-24)]">
   <div class="ds-card p-[var(--spacer-16)]">
    <SectionHeading text="系统管理" />
    <QuickActionGrid>
     <QuickActionItem
      v-for="action in systemVisible"
      :key="action.label"
      :icon="action.icon"
      :label="action.label"
      :badge="action.badge"
      @click="goRoute(action.routeName)"
     />
    </QuickActionGrid>
   </div>
  </section>
 </div>
</template>

<style scoped ponytail:allow-scoped-css 页面专属样式，折中>
/* ── 顶部问候区 ── */
.dashboard-greeting {
  background: linear-gradient(180deg, var(--bg-base-secondary) 0%, transparent 100%);
}

/* ── 头像 Logo 放大 + 品牌光晕 ── */
.dashboard-avatar :deep(.brand-logo__icon-wrapper) {
  width: 48px;  /* style-guard:ignore dashboard-only logo */
  height: 48px; /* style-guard:ignore dashboard-only logo */
  box-shadow: var(--shadow-glow-sm);
}

/* ── 三个功能区卡片风格统一：无边框 + 统一尺寸 ── */
.dashboard-section :deep(.ds-action-card) {
  border: none;
  box-shadow: none;
}
</style>
