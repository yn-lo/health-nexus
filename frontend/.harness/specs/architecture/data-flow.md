# 数据流规范

## 请求流

```
View (.vue)
  → shared/api/*.ts（业务接口函数）
    → shared/api/client.ts（ofetch 实例，统一拦截器与错误处理）
      → 后端 REST API（/api/*，开发环境由 Vite proxy 转发至 localhost:5230）
```

- 视图层不直接调用 ofetch，必须通过 `shared/api/` 下的接口函数发起请求
- `client.ts` 负责创建 ofetch 实例、注入认证令牌、统一处理响应与错误
- 各业务模块（auth、chat、wiki、staffChat、config）按领域拆分 API 文件
- 请求与响应的类型定义统一放在 `shared/types/` 中

## 认证流

```
用户登录（shared/views/Login.vue — 统一登录页 /login）
  → shared/api/auth.ts 发起统一登录请求（POST /api/auth/login）
    → stores/auth.ts 存储令牌与用户信息
      → 路由守卫（shared/utils/route-guard.ts）在导航前校验令牌
        → 未认证：重定向至统一登录页 /login
        → 已认证但角色不匹配：重定向至对应门户首页（跨 MPA 用 location.href）
```

- 统一登录页挂载在 chat SPA 的 `/login` 路由，由 Vite `mpaFallback` 映射到 `chat.html`
- 登录成功后根据用户角色自动路由：医护角色 → `/staff`（跨 MPA），患者 → `/chat`
- 令牌持久化由 `stores/auth.ts` 管理
- 路由守卫注册在各门户的 router 中，但守卫逻辑实现在 `shared/utils/route-guard.ts`
- 角色常量定义在 `shared/constants/roles.ts`

## 状态流

```
Pinia stores（stores/auth.ts, stores/chat.ts）
  → composables（chat/composables/, shared/composables/）
    → views（各页面组件）
```

- `stores/auth.ts`：全局认证状态，包括令牌、用户信息、登录/登出动作
- `stores/chat.ts`：聊天会话状态，包括会话列表、当前消息、SSE 连接状态
- composables 封装可复用的状态访问与副作用逻辑，是 views 与 stores/api 之间的桥梁
- 组件本地状态使用 `ref`/`reactive`，仅跨组件共享的状态才提升到 Pinia store
