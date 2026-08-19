# 约束执行工具索引

| 约束类别 | 工具 | 说明 |
|---------|------|------|
| 架构约束 | `tests/arch/governance.test.ts` | 22 条规则，AC-ARCH-FE-01 至 AC-ARCH-FE-22 |
| 样式约束 | `scripts/style-guard.mjs` | 5 条规则，R1 至 R5 |
| Lint 约束 | `eslint.config.js` | TypeScript + Vue 规则 |
| 类型约束 | `tsconfig.json` | strict 模式 |
| 依赖漏洞 | `npm audit --audit-level=high` | 阻断 high/critical 漏洞 |
| 死代码 | `knip`（`knip.json`） | 未使用导出/依赖 |
| 代码克隆 | `jscpd src/` | 重复代码（P2 非阻塞） |

## 门禁命令

```bash
.harness/constraints/ci/gate.sh          # 全跑 P0+P1+P2
.harness/constraints/ci/gate.sh p0       # 静态分析/类型/测试/构建/死代码
.harness/constraints/ci/gate.sh p1       # npm audit 安全
.harness/constraints/ci/gate.sh p2       # jscpd 工程债
```

等价于手动逐条执行：

```bash
npm run lint && npm run type-check && npm run test:arch && npm run lint:style \
  && npm run build && npm test && npm run dead-code && npm audit --audit-level=high
```

门禁分层：P0 阻塞（lint / type-check / test:arch / test / build / lint:style / dead-code）、P1 阻塞（npm audit）、P2 非阻塞告警（jscpd 代码克隆）。
