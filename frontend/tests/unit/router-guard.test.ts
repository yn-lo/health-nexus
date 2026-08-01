/**
 * 路由守卫集成测试
 * 测试认证守卫在未登录时正确重定向
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'
import { setupAuthGuards } from '@/router/guards'
import { clearTokens, setTokens } from '@/shared/api/client'

describe('路由守卫', () => {
  function createTestRouter() {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', name: 'home', component: { template: '<div>home</div>' } },
        { path: '/login', name: 'chat-login', component: { template: '<div>login</div>' } },
        { path: '/protected', name: 'protected', component: { template: '<div>protected</div>' }, meta: { requiresAuth: true } },
      ],
    })
    setupAuthGuards(router, 'chat-login')
    return router
  }

  beforeEach(() => {
    clearTokens()
  })

  it('未认证时访问受保护路由应重定向到登录页', async () => {
    const router = createTestRouter()
    router.push('/protected')
    await router.isReady()

    // 守卫应拦截并重定向
    expect(router.currentRoute.value.name).toBe('chat-login')
  })

  it('已认证时访问受保护路由应放行', async () => {
    setTokens('valid-token', 'valid-refresh')
    const router = createTestRouter()
    router.push('/protected')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('protected')
  })

  it('访问公开路由不需要认证', async () => {
    const router = createTestRouter()
    router.push('/login')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('chat-login')
  })
})
