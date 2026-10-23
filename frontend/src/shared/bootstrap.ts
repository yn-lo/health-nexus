/**
 * 双 SPA 统一引导
 * staff/chat 两个入口共用：全局错误捕获 + pinia + router + 认证守卫，消除 bootstrap 重复（jscpd clone）。
 */
import type { App } from 'vue'
import { createPinia } from 'pinia'
import type { Router } from 'vue-router'
import { setupAuthGuards } from '@/shared/utils/route-guard'
import { useBrandStore } from '@/stores/brand'

interface BootstrapOptions {
  /** 登录路由名。chat 端传 'login'（站内跳转）；staff 端不传（跨 MPA 跳转 /login） */
  loginRouteName?: string
  /** 浏览器标题前缀（患者端 / 医护端）；不传则仅用站点标题 */
  titlePrefix?: string
}

/** 统一应用引导（返回 app 以便入口调用 mount）。 */
export function bootstrapApp(app: App, router: Router, opts: BootstrapOptions = {}): App {
  // 全局错误捕获：render/computed/lifecycle 中未捕获的错误统一记录（不记录 PII）
  // ponytail: 前端无 slog 等价物，console.error 是 AC-ARCH-FE-10 唯一允许的 console 输出，折中
  app.config.errorHandler = (err, _instance, info) => {
    console.error('[Vue Error]', info, err)
  }
  app.use(createPinia())
  app.use(router)
  setupAuthGuards(router, opts.loginRouteName)
  // 品牌标识：拉取公开配置并覆盖浏览器标题/favicon（失败静默回退默认值，不阻塞渲染）
  const brandStore = useBrandStore()
  brandStore.setTitlePrefix(opts.titlePrefix ?? '')
  void brandStore.ensureLoaded()
  return app
}
