import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { setupAuthGuards } from '@/router/guards'

import '@/assets/styles/main.css'
import '@/assets/styles/staff-theme.css'

const app = createApp(App)
// 全局错误捕获：render/computed/lifecycle 中未捕获的错误统一记录（不记录 PII）
// ponytail: 前端无 slog 等价物，console.error 是 AC-ARCH-FE-10 唯一允许的 console 输出，折中
app.config.errorHandler = (err, _instance, info) => {
  console.error('[Vue Error]', info, err)
}
app.use(createPinia())
app.use(router)
setupAuthGuards(router) // staff SPA: /login 在 chat SPA，不传 loginRouteName 触发跨 MPA 跳转
app.mount('#staff-app')
