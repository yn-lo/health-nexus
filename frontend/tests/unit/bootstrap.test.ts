/**
 * bootstrapApp 单元测试
 * 覆盖：全局 errorHandler 设置、setupAuthGuards 调用与 loginRouteName 透传（chat/staff 差异）
 * 背景：chat/main.ts 与 staff/main.ts 的 bootstrap 重复（jscpd clone），提取为共享引导函数
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { bootstrapApp } from '@/shared/bootstrap'

vi.mock('@/router/guards', () => ({
  setupAuthGuards: vi.fn(),
}))

import { setupAuthGuards } from '@/router/guards'

const mockGuards = vi.mocked(setupAuthGuards)

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  })
}

describe('bootstrapApp', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('设置全局 errorHandler', () => {
    const app = createApp({ template: '<div />' })
    bootstrapApp(app, makeRouter())
    expect(app.config.errorHandler).toBeTypeOf('function')
  })

  it('调用 setupAuthGuards 并透传 loginRouteName（chat 端）', () => {
    const app = createApp({ template: '<div />' })
    bootstrapApp(app, makeRouter(), { loginRouteName: 'login' })
    expect(mockGuards).toHaveBeenCalledTimes(1)
    expect(mockGuards.mock.calls[0][1]).toBe('login')
  })

  it('未传 loginRouteName 时不传第二参数（staff 端跨 MPA 跳转）', () => {
    const app = createApp({ template: '<div />' })
    bootstrapApp(app, makeRouter())
    expect(mockGuards).toHaveBeenCalledTimes(1)
    expect(mockGuards.mock.calls[0][1]).toBeUndefined()
  })
})
