/**
 * 认证 Store 集成测试
 * 测试 Pinia auth store 的核心逻辑
 */
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import { getAccessToken, clearTokens, setTokens, getUserStored } from '@/shared/api/client'
import type { TokenUser } from '@/shared/types/auth'

const TEST_USER: TokenUser = { id: 1, username: 'testuser', role: 'PATIENT', avatar_url: '' }

// mock API 模块 — loginAndStore/registerAndStore 内部调用真实 setTokens（与生产逻辑一致）
vi.mock('@/shared/api/auth', () => ({
  loginAndStore: vi.fn().mockImplementation(async () => {
    setTokens('test-access', 'test-refresh')
    return { access: 'test-access', refresh: 'test-refresh', user: TEST_USER }
  }),
  registerAndStore: vi.fn().mockImplementation(async () => {
    setTokens('test-access', 'test-refresh')
    return { access: 'test-access', refresh: 'test-refresh', user: TEST_USER }
  }),
  logout: vi.fn().mockResolvedValue({ success: true }),
}))

describe('useAuthStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    clearTokens()
  })

  it('初始状态：未认证', () => {
    const store = useAuthStore()
    expect(store.user).toBeNull()
  })

  it('登录后应更新认证状态和用户信息', async () => {
    const store = useAuthStore()
    await store.login('testuser', 'password')

    expect(store.user).not.toBeNull()
    expect(store.user?.username).toBe('testuser')
    expect(getUserStored()?.username).toBe('testuser')
  })

  it('登出应清除状态', async () => {
    const store = useAuthStore()
    await store.login('testuser', 'password')
    expect(getAccessToken()).not.toBeNull()

    await store.logout()
    expect(store.user).toBeNull()
    expect(getAccessToken()).toBeNull()
    expect(getUserStored()).toBeNull()
  })

  it('注册后应更新认证状态', async () => {
    const store = useAuthStore()
    await store.register({ username: 'newuser', password: 'password123' })

    expect(store.user?.username).toBe('testuser')
  })
})
