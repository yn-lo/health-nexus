import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as authApi from '@/shared/api/auth'
import { clearTokens, getUserStored, setUserStored } from '@/shared/api/client'
import type { TokenUser, RegisterRequest } from '@/shared/types/auth'

export const useAuthStore = defineStore('auth', () => {
  // 初始化时从 localStorage 读取用户信息，避免页面刷新后路由守卫丢失角色
  const user = ref<TokenUser | null>(getUserStored())

  /** 统一登录 - 后端不校验角色，登录后由前端按 user.role 跳转 */
  async function login(username: string, password: string) {
    const res = await authApi.loginAndStore({ username, password })
    user.value = res.user
    setUserStored(res.user)
    return res
  }

  /** 用户注册 — 直接从注册响应填用户信息 */
  async function register(data: RegisterRequest) {
    const res = await authApi.registerAndStore(data)
    user.value = res.user
    setUserStored(res.user)
    return res
  }

  /** 登出 — 清理本地状态（包括 chat store） */
  async function logout() {
    try {
      await authApi.logout()
    } catch {
      // ponytail: 即使后端调用失败也清除本地状态，安全优先于一致性，折中
    }
    await clearAuth()
  }

  /**
   * 仅清理本地认证状态（不调用后端登出）。
   * 用于会话已在服务端失效的场景：改密后 token_version 递增，当前 access/refresh 立即失效，
   * 再调 /auth/logout 只会拿到 401（P2）。此时必须立刻清状态并跳登录页，
   * 否则用户停留在"看似已登录"的页面上，直到下一次请求 401 才被踢出。
   */
  async function clearAuth() {
    // 立即清空 user ref，缩短 UI 陈旧窗口
    user.value = null
    // 延迟导入避免循环依赖；chunk 加载失败时跳过 store 重置，clearTokens 仍保证本地状态清理
    try {
      const { useChatStore } = await import('@/stores/chat')
      useChatStore().$reset()
    } catch {
      // ponytail: 弱网下动态 import 可能失败，不应阻塞清理，折中
    }
    clearTokens()
  }

  /** 已登录用户修改密码（校验旧密码 + 新密码强度） */
  async function changePassword(oldPassword: string, newPassword: string) {
    return authApi.changePassword(oldPassword, newPassword)
  }

  /** 从服务端拉取最新个人资料并同步到本地 */
  async function fetchProfile() {
    const profile = await authApi.getProfile()
    user.value = profile
    setUserStored(profile)
    return profile
  }

  /** 更新个人资料并同步本地 */
  async function updateProfile(data: import('@/shared/types/auth').UpdateProfileRequest) {
    const profile = await authApi.updateProfile(data)
    user.value = profile
    setUserStored(profile)
    return profile
  }

  return { user, login, register, logout, clearAuth, changePassword, fetchProfile, updateProfile }
})
