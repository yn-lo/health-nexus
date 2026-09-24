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

唯一入口（完整实现，不要在别处重复定义检查范围）：

```bash
.harness/constraints/ci/gate.sh             # 全跑 P0+P1+P2
.harness/constraints/ci/gate.sh p0          # P0 静态分析/类型/测试/构建/死代码
.harness/constraints/ci/gate.sh p1          # P1 npm audit 安全
.harness/constraints/ci/gate.sh p2          # P2 jscpd 工程债
.harness/constraints/ci/gate.sh selftest    # 门禁自身验证（注入违规样例）
GATE_STRICT=1 .harness/constraints/ci/gate.sh   # CI：被跳过的检查视为失败
```

仓库根 `frontend/gate.sh`、`frontend/gate.ps1` 是同一入口的薄包装。

门禁分层：P0 阻塞（lint / type-check / test:arch / test / build / lint:style / dead-code）、P1 阻塞（npm audit）、P2 非阻塞告警（jscpd 代码克隆）。
