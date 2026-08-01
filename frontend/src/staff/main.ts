import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { bootstrapApp } from '@/shared/bootstrap'

import '@/assets/styles/main.css'
import '@/assets/styles/staff-theme.css'

bootstrapApp(createApp(App), router).mount('#staff-app') // staff SPA: /login 在 chat SPA，不传 loginRouteName 触发跨 MPA 跳转
