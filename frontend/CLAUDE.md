# Health Nexus Frontend 知识地图

> 本文件仅描述前端子项目。跨项目通用原则（MCP 工具优先策略、Ponytail、TDD 工作流、全局安全红线）见根目录 [../CLAUDE.md](../CLAUDE.md)。

## 项目概述

Health Nexus Frontend 是一个双 MPA（多页应用）健康平台，包含患者聊天门户（/chat）和员工管理门户（/staff）。技术栈为 Vue 3.5 + TypeScript + Vite 6 + Tailwind CSS 4 + Vant 4 + Pinia + vue-router 4 + ofetch。

## 知识导航

| 主题        | 规范文件                                               |
| --------- | -------------------------------------------------- |
| 架构总览      | .harness/specs/architecture/overview\.md           |
| 层级边界      | .harness/specs/architecture/boundaries.md          |
| 数据流       | .harness/specs/architecture/data-flow\.md          |
| 样式规范      | .harness/specs/conventions/styling.md              |
| 认证页设计语言  | design.md                                          |
| 约束工具      | .harness/constraints/README.md                     |
| 后端 API 端点（代码即文档） | 后端各域 `handler/router.go` + 契约测试 `backend/tests/api_contract_test.go` |

## 构建与验证

完整门禁：

```bash
npm run lint && npm run type-check && npm run test:arch && npm run lint:style && npm run dead-code && npm run dup-check
```

快速预检：

```bash
npm run type-check && npm run test:arch
```

## 硬性规则

1. 页面组件优先使用 src/assets/styles 中定义的全局样式（tokens.css / components.css / main.css），严禁手写组件级样式
2. chat/\* 和 staff/\* 严格隔离，禁止跨端 import
3. 视图层禁止直接 fetch/axios，必须通过 shared/api
4. 角色字面量必须用 shared/constants/roles.ts 常量
5. 禁止 console.log/debug/info（允许 warn/error）
6. 跨 MPA 跳转必须标注 ponytail:allow-location
7. 禁止魔法值
8. 禁止使用CDN

## 样式问题排查

遇到样式不生效（颜色/字号/边框等），先查 [.harness/specs/conventions/styling.md](.harness/specs/conventions/styling.md) 的「Tailwind 4 陷阱与规范」和「样式问题排查清单」两节。常见原因：

1. `@theme inline` 变量前缀错误（`--text-*` = 字号，`--color-text-*` = 颜色）
2. 组件样式被 unlayered preflight 覆盖（`components.css` 禁止包裹 `@layer`）
3. `var()` 无效值回退到继承色（禁止 `text-[var(--xxx-font-size)]`，用 `text-body-sm` 等）
4. Vite 代码分割导致 preflight 变成 unlayered
