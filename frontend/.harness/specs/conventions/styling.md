# 样式规范

## 设计令牌体系

`tokens.css` 定义所有 CSS 变量，作为全局唯一的样式令牌来源：

- `--text-*`：文本颜色令牌
- `--bg-*`：背景颜色令牌
- `--status-*`：状态颜色令牌（成功、警告、错误、信息等）
- `--ds-control-height-*`：控件高度令牌（sm / md / lg）
- `--spacer-*`：间距令牌
- `--radius-*`：圆角令牌

## 组件样式

`components.css` 定义所有 `.ds-*` 前缀的组件类：

- `.ds-btn`：按钮
- `.ds-card`：卡片
- `.ds-list-item`：列表项
- `.ds-fab`：悬浮操作按钮
- 其他 `.ds-*` 组件类

### 组件级样式豁免机制

原则上组件内禁止 `<style scoped>`（frontend/CLAUDE.md 硬性规则 1）。极少数场景（如第三方组件内部结构无法用工具类命中）可通过豁免通道：

- 在 `<style>` 块内标注 `ponytail:allow-scoped-css` 注释，由 `tests/arch/governance.test.ts` 的 AC-ARCH-FE-16 放行
- 未标注的 `<style scoped>` 会被架构约束测试拦截（当前仅 16 个组件使用，15 个已标注，1 个遗留待补标或改造）
- 豁免只允许"命中全局令牌 + 少量布局"的写法，仍禁止硬编码颜色/字号

## 布局样式

`main.css` 定义布局工具类及 Vant 主题覆盖。

## 主题覆写层（staff-theme.css）

医护端专属「高科技精品」（High-Tech Boutique）主题，仅在 `src/staff/main.ts` 中于 `main.css` 之后引入，chat 端不加载、零影响：

- **令牌覆写**：运行时覆写 `:root` 语义令牌——电光蓝品牌色、玻璃拟态半透明表面（`--bg-base-default`）、冷调边框与阴影、250ms 微交互；并新增主题专属令牌 `--ai-gradient`、`--ai-gradient-soft`、`--glass-blur`、`--ease-spring`
- **组件增强**：对 `.ds-*` 与 Vant 组件做渐变 / 磨玻璃 / 光晕增强，业务组件不新增控件级样式
- **字体**：展示字体 Space Grotesk 通过 `@fontsource-variable/space-grotesk` 本地引入（禁止 CDN），覆写 `--font-family-heading` / `--font-family-metric`
- **豁免**：作为令牌定义文件列入 `scripts/style-guard.mjs` 的 `ALLOWED_HEX_FILES` 白名单

## Tailwind 4 陷阱与规范

> **遇到样式不生效（颜色/字号/边框等）时，先查本节再排查。**

### 陷阱 1：`@theme inline` 变量前缀决定工具类类型

Tailwind 4 通过 `@theme inline` 中的变量前缀推断生成的工具类类型：

| 前缀 | 推断类型 | 生成的工具类 | 设置的 CSS 属性 |
|------|---------|-------------|---------------|
| `--text-*` | font-size | `text-*` | `font-size` + `line-height` |
| `--color-text-*` | color | `text-text-*` | `color` |
| `--color-bg-*` | color | `bg-bg-*` | `background-color` |
| `--spacing-*` | length | `p-*` / `m-*` / `gap-*` | `padding` / `margin` / `gap` |

**错误写法**：
```css
/* @theme inline 中缺少 --text-* 字号映射 */
@theme inline {
  --color-text-default: var(--text-default);  /* 只映射了颜色 */
  /* 没有 --text-body-sm 等字号映射 */
}
```
结果：`text-[var(--body-sm-font-size)]` 被解析为 `color: 11px`（无效值 → 回退到继承色 → 黑色）

**正确写法**：
```css
@theme inline {
  /* 字号映射（生成 text-body-sm 等） */
  --text-body-xs: var(--body-xs-font-size);
  --text-body-xs--line-height: var(--body-xs-line-height);
  --text-body-sm: var(--body-sm);
  --text-body-sm--line-height: var(--body-sm-line-height);
  /* ... */

  /* 颜色映射（生成 text-text-brand 等） */
  --color-text-default: var(--text-default);
  --color-text-brand: var(--text-brand);
  /* ... */
}
```

> 本项目 `main.css` 的实际 `@theme inline` 命名：`--color-brand / --color-base / --color-surface / --color-text / --color-icon / --color-border` 等（**未使用 `--color-bg-*` 前缀**），对应工具类为 `bg-brand`、`bg-base`、`text-text-brand`、`border-border` 等。查看真实映射以 `main.css` 的 `@theme inline` 块为准。

**规则**：
- 字号用 `text-body-sm` / `text-body-base` 等语义工具类，**禁止** `text-[var(--xxx-font-size)]`
- 颜色用 `text-text-brand` / `text-text-default` 等语义工具类，**禁止** `text-[var(--text-xxx)]`
- 行高由字号工具类自动包含（`--text-body-sm--line-height`），**禁止** 单独写 `leading-[var(--xxx-line-height)]`

### 陷阱 2：Unlayered 样式优先级高于所有 `@layer`

CSS Cascade Layers 规范：**unlayered 样式优先级高于任何 `@layer` 中的样式，无论特异性高低。**

Tailwind 4 的 preflight（`button { color: inherit }`、`input { font: inherit }` 等）在 Vite 代码分割后可能变成 unlayered。如果组件样式放在 `@layer components` 中，会被 unlayered 的 preflight 覆盖。

**症状**：`.ds-btn--primary { color: #fff }` 不生效，按钮文字仍是黑色。

**规则**：
- `components.css` **禁止** 包裹在 `@layer components` 中，必须保持 unlayered
- 组件样式通过特异性（`.ds-btn--primary` 比 preflight 的 `button` 更高）也无法获胜——unlayered 样式在层序上恒高于任何 `@layer`，根因是层序而非特异性。因此唯一解法是让 `.ds-*` 类本身保持 unlayered（位于 `@layer` 之外）
- Vant 覆写若需放入 `@layer components`（如 main.css 的 Vant 覆写块），必须确认被覆写的目标不在 unlayered 中，否则同样不生效

## 样式问题排查清单

遇到样式不生效（颜色/字号/边框/间距等）时，按下表从最常见原因开始排查：

| # | 症状 | 最常见原因 | 定位方法 |
|---|------|-----------|---------|
| 1 | 颜色不生效，回退到继承色/黑色 | `@theme inline` 变量前缀映射错误或缺失：`--text-*` 是字号，`--color-text-*` 才是颜色；项目未定义 `--color-bg-*` | 检查 `main.css` 的 `@theme inline` 块是否定义了对应 `--color-*`；禁止 `text-[var(--xxx)]` arbitrary 引用非颜色变量 |
| 2 | 颜色不生效，DevTools 显示样式被划掉 | 组件样式被 unlayered preflight 覆盖 | 检查是否把样式写在 `@layer components` 内（`components.css` 禁止）；Vite 代码分割会把 preflight 变 unlayered |
| 3 | 字号不生效 | 用了 `text-[var(--xxx-font-size)]` 被解析为 color | 改用语义工具类 `text-body-sm` / `text-body-base`；行高由 `--text-*-line-height` 自动附带 |
| 4 | 组件颜色类（`.ds-*`）不生效 | 同上（层序问题），或使用了不在 tokens.css 中的硬编码颜色 | 先确认令牌存在，再确认类位于 `@layer` 外 |
| 5 | 仅医护端样式不生效 | staff 主题变量未加载 | 确认 `staff-theme.css` 仅在 `src/staff/main.ts` 引入（chat 端不加载属预期）；令牌定义列入 `style-guard.mjs` 的 `ALLOWED_HEX_FILES` 白名单 |
| 6 | 构建后样式与 dev 不一致 | preflight 在代码分割后变 unlayered | 检查 `components.css` 是否被 `@layer` 包裹；构建产物对比 |

> 硬编码颜色：组件内直接写十六进制颜色会被 `scripts/style-guard.mjs` 拦截（`ALLOWED_HEX_FILES` 白名单外的文件一律拒绝），因此颜色必须来自 tokens.css 令牌。
