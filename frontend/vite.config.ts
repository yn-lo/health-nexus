/// <reference types="vitest/config" />
import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { resolve } from 'node:path'
import { readdirSync } from 'node:fs'

// 自动扫描入口 HTML 文件，构建 MPA 入口配置
function getMpaInputs(): Record<string, string> {
  const rootDir = __dirname
  const entries: Record<string, string> = {}
  for (const file of readdirSync(rootDir)) {
    if (file.endsWith('.html')) {
      const name = file.replace(/\.html$/, '')
      entries[name] = resolve(rootDir, file)
    }
  }
  return entries
}

/** MPA dev 路由中间件：浏览器导航请求按入口归属选择 HTML
 * - /staff、/staff/*、/styles → staff.html（医护端）
 * - 其余 HTML 导航 → chat.html（患者端）
 * 新增患者端顶级路由无需再维护路径白名单；静态资源/API/HTML 文件请求直接放行。 */
function mpaFallback(): Plugin {
  return {
    name: 'mpa-fallback',
    configureServer(server) {
      server.middlewares.use((req, _res, next) => {
        const url = req.url?.split('?')[0] ?? ''
        const isHtmlNavigation = req.method === 'GET' && req.headers.accept?.includes('text/html')
        if (!isHtmlNavigation || url.endsWith('.html')) return next()
        if (url.startsWith('/api/') || url.startsWith('/uploads/')) return next()
        if (url.startsWith('/staff') || url === '/styles') {
          req.url = '/staff.html'
          return next()
        }
        req.url = '/chat.html'
        next()
      })
    },
  }
}

export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    mpaFallback(),
  ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  // markdown-it 只在懒加载的会话路由中使用：不预构建会在首条消息渲染时才被发现，
  // 触发 Vite 整页 reload 并中断正在进行的 SSE 请求。
  optimizeDeps: {
    include: ['markdown-it'],
  },
  build: {
    rollupOptions: {
      input: getMpaInputs(),
    },
  },
  server: {
    host: 'localhost',
    proxy: {
      '/api': {
        target: 'http://localhost:5230',
        changeOrigin: true,
      },
      // 文章正文图片（后端本地存储目录，直接由 Go 静态服务托管）
      '/uploads': {
        target: 'http://localhost:5230',
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'happy-dom',
    include: ['tests/**/*.test.ts'],
    globals: true,
  },
})
