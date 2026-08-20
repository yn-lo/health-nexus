# Health Nexus 认证页设计语言（design.md）

> 本文档记录认证相关页面（登录 / 注册 / 忘记密码 / 修改密码）的「极简编辑风 + 上下分屏」设计语言，作为未来新增或改造认证页面的参考基准。
>
> 适用范围：`src/shared/views/` 下的 `Login.vue`、`Register.vue`、`ForgotPassword.vue`、`ChangePassword.vue`。
> 样式实现位置：`src/assets/styles/components.css` 的 `auth-*` 区块。

## 1. 设计方向

**极简编辑风 + 上下分屏** —— 大量留白、大字号、克制配色，靠排版与留白营造高级感，而非依赖装饰。

- **视觉重心在上半屏**：品牌叙事（大标题 + 价值主张），而非表单卡片
- **功能在下半屏**：登录 / 注册表单，直接铺在留白背景上
- **无卡片容器**：彻底摆脱「表单卡片」套路，表单呼吸在留白上
- **克制配色**：纯白背景 + 深色文字 + 单一品牌强调色（Indigo）
- **老年患者可读性优先**：大字号、清晰层级、下划线输入框聚焦时品牌色强调

## 2. 设计令牌

全部基于 `tokens.css` 语义令牌，浅色 / 暗色主题自动适配。认证页专属背景令牌：

```css
/* components.css .auth-page */
.auth-page {
  --auth-bg: var(--bg-base-default); /* 纯白留白背景 */
}
```

关键令牌引用：

| 用途 | 令牌 |
| ---- | ---- |
| 背景 | `var(--bg-base-default)`（纯白） |
| 主文字 | `var(--text-default)` |
| 次要文字 | `var(--text-secondary)` / `var(--text-tertiary)` |
| 品牌强调色 | `var(--bg-brand)` / `var(--bg-brand-hover)` |
| 输入框下划线 | `var(--border-neutral-l2)`（聚焦 `var(--bg-brand)`） |
| 顶部光晕 | `var(--hero-glow-color-soft)` |

## 3. 布局结构（上下分屏）

```
┌──────────────────────────────┐
│  auth-aura--top（顶部光晕）    │
│                              │
│  ┌────────────────────────┐  │
│  │ 品牌行：小Logo + 品牌名  │  │  ← auth-brand-row
│  │                        │  │
│  │ 大标题（36px+）          │  │  ← auth-title
│  │ 副标题（价值主张）        │  │  ← auth-subtitle
│  │ （可选）步骤指示器        │  │
│  └────────────────────────┘  │  ← auth-hero（上半屏，靠下对齐）
│                              │
│  ┌────────────────────────┐  │
│  │ 字段标签 + 下划线输入框   │  │  ← auth-field / auth-label
│  │ 字段标签 + 下划线输入框   │  │
│  │ 忘记密码 / 错误提示       │  │
│  │ 胶囊主按钮               │  │  ← auth-submit-btn
│  │                        │  │
│  │ 底部链接（注册/登录）      │  │  ← auth-footer
│  └────────────────────────┘  │  ← auth-form-area（下半屏，垂直居中）
└──────────────────────────────┘
```

容器结构（所有认证页统一）：

```html
<PageShell
  :bottom-nav="false"
  :padded="false"
  background="var(--auth-bg)"
  class="auth-page relative overflow-hidden"
>
  <div class="auth-aura auth-aura--top" aria-hidden="true" />
  <div class="auth-scroll relative mx-auto flex min-h-dvh w-full max-w-[400px] flex-col px-[var(--spacer-24)]">
    <!-- 上半屏 -->
    <header class="auth-hero flex flex-col justify-end pb-[var(--spacer-24)]">…</header>
    <!-- 下半屏 -->
    <section class="auth-form-area flex flex-1 flex-col justify-center pb-[var(--spacer-16)]">…</section>
  </div>
</PageShell>
```

## 4. 组件规范

### 4.1 品牌行（auth-brand-row / auth-brand-name）

小 Logo + 字距大写的品牌名，左上角对齐：

```html
<div class="auth-brand-row flex items-center gap-[var(--spacer-10)]">
  <BrandLogo size="sm" hide-name />
  <span class="auth-brand-name font-heading text-body-sm-strong tracking-[0.14em] text-text-tertiary">
    HEALTH NEXUS
  </span>
</div>
```

### 4.2 大标题（auth-title）

编辑风大字号，可换行强调：

```html
<h1 class="auth-title font-heading font-semibold leading-[1.15] tracking-[-0.02em] text-text">
  智能健康<br>宣教平台
</h1>
```

```css
.auth-title {
  font-size: clamp(2.25rem, 9vw, 3rem); /* 约 36-48px */
  line-height: 1.15;
}
```

### 4.3 字段块（auth-field / auth-label）

标签在上、输入框在下，标签为小号灰色字距：

```html
<div class="auth-field">
  <label class="auth-label">用户名</label>
  <div class="ds-field-wrap ds-field-wrap--underline">
    <input id="auth-username" v-model="username" placeholder="请输入用户名或手机号" autocomplete="username" aria-label="用户名">
  </div>
</div>
```

```css
.auth-field {
  display: flex;
  flex-direction: column;
  gap: var(--spacer-8);
}
.auth-label {
  font-size: var(--body-sm-font-size);
  font-weight: var(--font-weight-medium);
  color: var(--text-tertiary);
  letter-spacing: 0.04em;
}
```

### 4.4 下划线输入框（auth-form .ds-field-wrap）

无边框、底部细线、聚焦品牌色。表单需加 `auth-form` 类：

```css
.auth-form .ds-field-wrap {
  min-height: var(--ds-control-height-lg);
  border: none;
  border-bottom: 1px solid var(--border-neutral-l2);
  border-radius: 0;
  background: transparent;
  padding: var(--spacer-6) 0;
  box-shadow: none;
}
.auth-form .ds-field-wrap:focus-within {
  border-bottom-color: var(--bg-brand);
  box-shadow: none;
}
```

### 4.5 胶囊主按钮（auth-submit-btn）

圆角胶囊 + 品牌实底，无阴影：

```css
.auth-submit-btn {
  height: var(--ds-control-height-lg) !important;
  border-radius: var(--radius-full) !important;
  background: var(--bg-brand) !important;
  border-color: var(--bg-brand) !important;
  box-shadow: none !important;
  font-size: var(--body-base-font-size);
  font-weight: var(--font-weight-strong);
  letter-spacing: 0.08em;
}
```

用法：`<DsSubmitButton :loading="loading" text="登 录" class="auth-submit-btn" />`

### 4.6 次按钮（auth-guest-btn）

极简文字按钮，无边框：

```css
.auth-guest-btn {
  border: none;
  background: transparent;
  color: var(--text-tertiary);
  /* hover 时浅色底 + 深色文字 */
}
```

### 4.7 底部链接（auth-footer）

```html
<footer class="auth-footer mt-[var(--spacer-24)] flex flex-col items-center gap-[var(--spacer-12)] text-center">
  <p class="text-body-base text-text-secondary">
    还没有账号？
    <button class="ds-link-btn font-medium" @click="goRegister">立即注册</button>
  </p>
</footer>
```

## 5. 页面清单

| 页面 | 文件 | 布局特点 |
| ---- | ---- | ---- |
| 登录 | `Login.vue` | 标准上下分屏，上半屏品牌叙事 + 下半屏登录表单 |
| 注册 | `Register.vue` | 标准上下分屏，表单含用户名/密码/确认密码/协议 |
| 忘记密码 | `ForgotPassword.vue` | 上下分屏 + 步骤指示器（1 请求 / 2 设置 / 3 完成） |
| 修改密码 | `ChangePassword.vue` | 保留 AppHeader 返回导航（已登录页面），无品牌叙事区，仅极简表单 |

## 6. 可访问性约定

- 输入框必须有 `aria-label`（DsPasswordField 内部已内置）
- 直接 `<input>` 的字段，`<label>` 用 `for` 关联 `id`；DsPasswordField 包裹的字段，label 不设 `for`（内部 input 无 id，靠 aria-label 提供可访问名称）
- 触摸目标 ≥ 44px（`var(--touch-target-min)`）
- 大字号（`var(--reading-font-size-a11y)` = 16px）保证老年患者可读性
- 尊重 `prefers-reduced-motion`（光晕动画在 reduce 时关闭）

## 7. 硬性约束

1. **不写组件级 scoped 样式**：认证页样式统一放 `components.css` 的 `auth-*` 区块，遵循 `frontend/CLAUDE.md` 硬性规则 1
2. **全部基于 tokens.css 令牌**：禁止魔法值（style-guard 会拦截）
3. **与 Register / ForgotPassword / ChangePassword 共用** PageShell + 设计令牌，保持全站认证流程一致

## 8. 验证方式

```bash
npm run type-check   # 类型检查
npm run lint         # ESLint
npm run lint:style   # style-guard 样式门禁（禁止魔法值等）
```

移动端渲染验证（Playwright）：

```bash
playwright-cli open --mobile http://localhost:5173/chat/login
```

关键检查点：背景纯白、无卡片容器、下划线输入框（borderRadius 0）、胶囊按钮（999px）、大字号标题（36px+）。
