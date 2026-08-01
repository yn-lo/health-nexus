import type { Router } from 'vue-router'
import { getAccessToken } from '@/shared/api/client'

/**
 * 注册路由认证守卫
 * @param router 路由实例
 * @param loginRouteName 未认证时跳转的路由名称（同 SPA 内）；不传则跨 MPA 跳转 /login
 */
export function setupAuthGuards(router: Router, loginRouteName?: string) {
  router.beforeEach((to) => {
    if (to.meta.requiresAuth && !getAccessToken()) {
      if (loginRouteName) {
        // 保留原始目标路径，登录成功后跳回（Login.vue 读取 route.query.redirect）
        return { name: loginRouteName, query: { redirect: to.fullPath } }
      }
      // staff SPA: /login 在 chat SPA，需跨 MPA 跳转
      window.location.href = `/login?redirect=${encodeURIComponent(to.fullPath)}` // ponytail:allow-location 跨 MPA 跳转
    }
  })
}
