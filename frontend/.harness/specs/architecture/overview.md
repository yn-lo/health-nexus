# 系统概览

Health Nexus 前端是一个双 MPA（多页应用）架构的 Vue 3 项目，包含患者聊天门户和员工管理门户两个独立入口，共享底层基础设施。

## 技术栈

| 类别 | 技术 | 版本 |
|------|------|------|
| 框架 | Vue | 3.5+ |
| 语言 | TypeScript | latest |
| 构建 | Vite | 6 |
| 样式 | Tailwind CSS | 4 |
| UI 组件库 | Vant | 4 |
| 状态管理 | Pinia | 2 |
| 路由 | vue-router | 4 |
| HTTP 客户端 | ofetch | latest |
| 测试 | Vitest + happy-dom | Vitest 4 / happy-dom 20 |

## 模块划分

| 目录 | 职责 |
|------|------|
| `src/chat/` | 患者聊天门户，包含聊天视图、SSE 对话、科室查询等患者侧功能 |
| `src/staff/` | 员工管理门户，包含文章管理、AI 配置、安全规则、危机事件等管理侧功能 |
| `src/shared/` | 共享基础设施层 |
| ├─ `api/` | 后端 REST API 封装（ofetch 客户端 + 各业务接口模块） |
| ├─ `components/` | 跨门户复用的 UI 组件 |
| ├─ `composables/` | 跨门户复用的组合式函数 |
| ├─ `constants/` | 角色定义、Vant 主题等全局常量 |
| ├─ `types/` | 全局 TypeScript 类型定义 |
| ├─ `utils/` | 工具函数（含路由守卫） |
| ├─ `layouts/` | 页面布局壳（BottomNavLayout、ChatLayout、StaffLayout） |
| └─ `views/` | 跨门户共享页面（统一登录、注册、忘记密码） |
| `src/stores/` | Pinia 全局状态（auth 认证状态、chat 会话状态） |
| `src/assets/styles/` | 设计令牌（tokens.css）、组件样式（components.css）、主样式（main.css）、医护端主题覆写（staff-theme.css，仅 staff 端加载） |

## 构建拓扑

Vite 以 MPA 模式构建，通过扫描项目根目录的 HTML 文件自动生成入口：

- `chat.html` → `/chat` 路由前缀 → `src/chat/main.ts`
- `staff.html` → `/staff` 路由前缀 → `src/staff/main.ts`

开发服务器通过 `mpaFallback` 中间件将 URL 前缀映射到对应 HTML 入口。
两个入口各自拥有独立的 vue-router 实例和 App.vue，但共享 `src/shared/` 和 `src/stores/` 中的代码。
构建产物通过 Rollup 的 code-splitting 自动提取共享 chunk。
