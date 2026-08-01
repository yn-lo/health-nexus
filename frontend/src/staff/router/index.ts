import { createRouter, createWebHistory } from 'vue-router'
import StaffLayout from '@/shared/layouts/StaffLayout.vue'
import { staffRouteGuard, adminRouteGuard, superAdminRouteGuard } from '@/shared/utils/route-guard'

/** 医护端路由配置 */
const routes = [
  {
    path: '/staff',
    component: StaffLayout,
    beforeEnter: staffRouteGuard,
    children: [
      {
        path: '',
        name: 'staff-dashboard',
        component: () => import('@/staff/views/StaffDashboard.vue'),
      },
      {
        path: 'articles',
        name: 'staff-articles',
        component: () => import('@/staff/views/ArticleManagement.vue'),
      },
      {
        path: 'articles/new',
        name: 'staff-article-create',
        component: () => import('@/staff/views/ArticleForm.vue'),
      },
      {
        path: 'articles/:id/edit',
        name: 'staff-article-edit',
        component: () => import('@/staff/views/ArticleForm.vue'),
      },
      {
        path: 'articles/review',
        name: 'staff-article-review',
        component: () => import('@/staff/views/ArticleReview.vue'),
      },
      {
        path: 'references',
        name: 'staff-references',
        component: () => import('@/staff/views/ReferenceManagement.vue'),
      },
      {
        path: 'crisis-events',
        name: 'staff-crisis-events',
        component: () => import('@/staff/views/CrisisEventList.vue'),
      },
      {
        path: 'profile',
        name: 'staff-profile',
        component: () => import('@/staff/views/StaffProfile.vue'),
      },
      // 系统配置（仅 SUPER_ADMIN / DEPT_ADMIN）
      {
        path: 'profile/config',
        name: 'staff-config-home',
        component: () => import('@/staff/views/config/ConfigHome.vue'),
        beforeEnter: adminRouteGuard,
      },
      {
        path: 'profile/config/accounts',
        name: 'staff-config-accounts',
        component: () => import('@/staff/views/config/AccountConfig.vue'),
        beforeEnter: adminRouteGuard,
      },
      {
        path: 'profile/config/accounts/:id',
        name: 'staff-config-account-detail',
        component: () => import('@/staff/views/config/AccountDetail.vue'),
        beforeEnter: adminRouteGuard,
      },
      {
        path: 'profile/config/ai-providers',
        name: 'staff-config-ai-providers',
        component: () => import('@/staff/views/config/AIProviderConfig.vue'),
        beforeEnter: superAdminRouteGuard,
      },
      {
        path: 'profile/config/ai-providers/new',
        name: 'staff-config-ai-provider-create',
        component: () => import('@/staff/views/config/AIProviderForm.vue'),
        beforeEnter: superAdminRouteGuard,
      },
      {
        path: 'profile/config/ai-providers/:id/edit',
        name: 'staff-config-ai-provider-edit',
        component: () => import('@/staff/views/config/AIProviderForm.vue'),
        beforeEnter: superAdminRouteGuard,
      },
      {
        path: 'profile/config/sensitive-words',
        name: 'staff-config-sensitive-words',
        component: () => import('@/staff/views/config/SensitiveWordConfig.vue'),
        beforeEnter: superAdminRouteGuard,
      },
      {
        path: 'profile/config/safety-policy',
        name: 'staff-config-safety-policy',
        component: () => import('@/staff/views/config/SafetyPolicyOverview.vue'),
        beforeEnter: superAdminRouteGuard,
      },
      {
        path: 'profile/config/safety-rules',
        name: 'staff-config-safety-rules',
        component: () => import('@/staff/views/config/SafetyRuleConfig.vue'),
        beforeEnter: superAdminRouteGuard,
      },
      {
        path: 'profile/config/rag',
        name: 'staff-config-rag',
        component: () => import('@/staff/views/config/RAGConfig.vue'),
        beforeEnter: adminRouteGuard,
      },
      {
        path: 'profile/config/safety-messages',
        name: 'staff-config-safety-messages',
        component: () => import('@/staff/views/config/SafetyMessageConfig.vue'),
        beforeEnter: superAdminRouteGuard,
      },
      {
        path: 'profile/config/prompts',
        name: 'staff-config-prompts',
        component: () => import('@/staff/views/config/PromptTemplateConfig.vue'),
        beforeEnter: adminRouteGuard,
      },
      {
        path: 'profile/config/prompts/new',
        name: 'staff-config-prompt-create',
        component: () => import('@/staff/views/config/PromptTemplateDetail.vue'),
        beforeEnter: adminRouteGuard,
      },
      {
        path: 'profile/config/prompts/:id',
        name: 'staff-config-prompt-detail',
        component: () => import('@/staff/views/config/PromptTemplateDetail.vue'),
        beforeEnter: adminRouteGuard,
      },
      {
        path: 'profile/config/audit-logs',
        name: 'staff-config-audit-logs',
        component: () => import('@/staff/views/config/AuditLogConfig.vue'),
        beforeEnter: superAdminRouteGuard,
      },
      {
        path: 'profile/config/departments',
        name: 'staff-config-departments',
        component: () => import('@/staff/views/config/DepartmentConfig.vue'),
        beforeEnter: superAdminRouteGuard,
      },
    ],
    meta: { requiresAuth: true },
  },
  // 全局样式预览（无需认证，项目级共享）
  {
    path: '/styles',
    name: 'global-styles',
    component: () => import('@/staff/views/dev/StyleShowcase.vue'),
  },
  // 法律文档（/terms、/privacy）统一由 chat SPA 提供，staff SPA 不再重复注册。
  // 需查看时通过 <a href="/terms"> 跨 MPA 跳转（见 Register.vue）。
  // 认证路由（无布局）- 统一登录页在 /login（chat SPA），此处仅保留重定向
  {
    path: '/staff/login',
    redirect: '/login',
  },
  {
    path: '/staff/register',
    name: 'staff-register',
    component: () => import('@/shared/views/Register.vue'),
  },
  {
    path: '/staff/forgot-password',
    name: 'staff-forgot-password',
    component: () => import('@/shared/views/ForgotPassword.vue'),
  },
  {
    path: '/staff/change-password',
    name: 'staff-change-password',
    component: () => import('@/shared/views/ChangePassword.vue'),
    beforeEnter: staffRouteGuard,
  },
  {
    path: '/staff/profile/edit',
    name: 'staff-edit-profile',
    component: () => import('@/shared/views/EditProfile.vue'),
    beforeEnter: staffRouteGuard,
  },
  // 404 兜底：未匹配的 /staff/* 重定向到首页
  { path: '/staff/:pathMatch(.*)*', redirect: '/staff' },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})
