# 约束执行工具索引

| 约束类别 | 工具 | 说明 |
|---------|------|------|
| 架构约束 | `tests/arch/governance.test.ts` | 20 条规则，AC-ARCH-FE-01 至 AC-ARCH-FE-20 |
| 样式约束 | `scripts/style-guard.mjs` | 5 条规则，R1 至 R5 |
| Lint 约束 | `eslint.config.js` | TypeScript + Vue 规则 |
| 类型约束 | `tsconfig.json` | strict 模式 |

## 门禁命令

```bash
npm run lint && npm run type-check && npm run test:arch && npm run lint:style
```
