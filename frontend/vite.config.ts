/// <reference types="vitest/config" />
import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import Components from 'unplugin-vue-components/vite'
import { VantResolver } from '@vant/auto-import-resolver'
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

/** MPA dev 路由中间件：将 URL 前缀映射到对应 SPA 入口 HTML
 * - /staff/* + /styles → staff.html（医护端）
 * - /、/login、/chat/*、/wiki/*、/about、/terms、/privacy → chat.html（患者端）
 * 未列出的路径由 vite 默认 fallback 处理（命中 index.html 跳转） */
function mpaFallback(): Plugin {
  return {
    name: 'mpa-fallback',
    configureServer(server) {
      server.middlewares.use((req, _res, next) => {
        const url = req.url?.split('?')[0] ?? ''
        // staff SPA 入口
        if (url.startsWith('/staff/') || url === '/staff' || url === '/styles') {
          req.url = '/staff.html'
          return next()
        }
        // chat SPA 入口（含法律文档与关于页等顶级路由）
        if (
          url === '/' || url === '/login' ||
          url === '/chat' || url.startsWith('/chat/') ||
          url === '/wiki' || url.startsWith('/wiki/') ||
          url === '/about' || url === '/terms' || url === '/privacy'
        ) {
          req.url = '/chat.html'
          return next()
        }
        next()
      })
    },
  }
}

export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    Components({
      resolvers: [VantResolver({ importStyle: false })],
    }),
    mpaFallback(),
  ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  build: {
    rollupOptions: {
      input: getMpaInputs(),
      external: ['mermaid'],
    },
  },
  server: {
    host: '0.0.0.0',
    proxy: {
      '/api': {
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
