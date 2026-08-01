import { createRouter, createWebHistory } from 'vue-router'
import ChatLayout from '@/shared/layouts/ChatLayout.vue'
import { patientRouteGuard } from '@/shared/utils/route-guard'

/** 患者端路由配置 */
const routes = [
  {
    path: '/',
    redirect: '/chat',
  },
  // ===== 聊天门户 =====
  {
    path: '/chat',
    component: ChatLayout,
    beforeEnter: patientRouteGuard,
    meta: { allowStaffPreview: true },
    children: [
      {
        path: '',
        name: 'chat-home',
        component: () => import('@/chat/views/ChatHome.vue'),
      },
      {
        path: 'conversation/:id?',
        name: 'chat-conversation',
        component: () => import('@/chat/views/ChatConversation.vue'),
      },
      {
        path: 'profile',
        name: 'personal-center',
        component: () => import('@/chat/views/PersonalCenter.vue'),
        meta: { requiresAuth: true },
      },
    ],
  },
  // ===== 知识库门户（独立顶级路由，复用 ChatLayout 底部导航） =====
  {
    path: '/wiki',
    component: ChatLayout,
    beforeEnter: patientRouteGuard,
    // 公开科普内容：允许已登录医护只读预览（守卫放行），不重定向到 /staff
    meta: { allowStaffPreview: true },
    children: [
      {
        path: '',
        name: 'wiki-list',
        component: () => import('@/chat/views/KnowledgeList.vue'),
      },
      {
        path: 'article/:id',
        name: 'wiki-article',
        component: () => import('@/chat/views/ArticleDetail.vue'),
      },
    ],
  },
  // ===== 关于页面（独立顶级路由，复用 ChatLayout） =====
  {
    path: '/about',
    component: ChatLayout,
    beforeEnter: patientRouteGuard,
    meta: { allowStaffPreview: true },
    children: [
      {
        path: '',
        name: 'about-us',
        component: () => import('@/chat/views/AboutUs.vue'),
      },
    ],
  },
  // ===== 用户协议（独立顶级路由，复用 ChatLayout） =====
  {
    path: '/terms',
    component: ChatLayout,
    beforeEnter: patientRouteGuard,
    meta: { allowStaffPreview: true },
    children: [
      {
        path: '',
        name: 'terms-of-service',
        component: () => import('@/shared/views/TermsOfService.vue'),
      },
    ],
  },
  // ===== 隐私政策（独立顶级路由，复用 ChatLayout） =====
  {
    path: '/privacy',
    component: ChatLayout,
    beforeEnter: patientRouteGuard,
    meta: { allowStaffPreview: true },
    children: [
      {
        path: '',
        name: 'privacy-policy',
        component: () => import('@/shared/views/PrivacyPolicy.vue'),
      },
    ],
  },
  // 认证路由（无布局）
  {
    path: '/login',
    name: 'login',
    component: () => import('@/shared/views/Login.vue'),
  },
  {
    path: '/chat/login',
    redirect: '/login',
  },
  {
    path: '/chat/register',
    name: 'chat-register',
    component: () => import('@/shared/views/Register.vue'),
  },
  {
    path: '/chat/forgot-password',
    name: 'chat-forgot-password',
    component: () => import('@/shared/views/ForgotPassword.vue'),
  },
  {
    path: '/chat/change-password',
    name: 'chat-change-password',
    component: () => import('@/shared/views/ChangePassword.vue'),
    beforeEnter: patientRouteGuard,
    meta: { requiresAuth: true },
  },
  {
    path: '/chat/profile/edit',
    name: 'chat-edit-profile',
    component: () => import('@/shared/views/EditProfile.vue'),
    beforeEnter: patientRouteGuard,
    meta: { requiresAuth: true },
  },
  // 旧 /chat/about 重定向到 /about
  { path: '/chat/about', redirect: '/about' },
  // 404 兜底：未匹配的 /chat/* 重定向到首页
  { path: '/chat/:pathMatch(.*)*', redirect: '/chat' },
  // 404 兜底：未匹配的 /wiki/* 重定向到知识库
  { path: '/wiki/:pathMatch(.*)*', redirect: '/wiki' },
]

export const router = createRouter({
  history: createWebHistory(),
  routes
})
