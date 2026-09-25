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

## 门禁规则索引

每个阻断规则必须可查到：规则 ID、守护的风险、适用范围、执行阶段、验证入口、判定标准、维护者、例外策略。

- **维护者**：frontend-team。
- **阈值与参数**以 `.harness/constraints/ci/gate.sh`、`.jscpd.json`、`knip.json`、`eslint.config.js` 为准；本表只登记归属与判定，不重复参数。
- **执行阶段**：本地预检 / 合并门禁 / 发布门禁 / 定期检查。发布门禁用 `GATE_STRICT=1`，被跳过的检查按失败处理。
- **结果状态**：PASS / FAIL / ERROR / SKIP / WAIVED，**SKIP 不等于 PASS**（没有运行不得记成通过）。

| 规则 ID | 守护的风险 | 阶段 | 验证入口 | 判定 / 例外策略 |
|---|---|---|---|---|
| FE-LINT | 调试输出残留（no-console）、未用变量等 | 合并 | P0-1 `npm run lint` | 失败即阻断 |
| FE-TYPE | 类型错误（strict 模式） | 合并 | P0-2 `npm run type-check` | 失败即阻断 |
| AC-ARCH-FE-01..22 | 双端跨端 import、视图层直连 fetch、角色字面量、CDN 引用、shared 反向依赖、死代码等 | 合并 | P0-3 `npm run test:arch` | 任一违规即阻断 |
| FE-UNIT | 功能回归 | 合并 | P0-4 `npm test` | 失败即阻断 |
| FE-BUILD | 构建失败（MPA 多入口产物不可用） | 合并 | P0-5 `npm run build` | 失败即阻断 |
| FE-STYLE R1-R5 | 硬编码色值 / 控件尺寸、原始色板、单边主题边框、scoped 控件样式 | 合并 | P0-6 `npm run lint:style` | R1/R3/R4 为 error 级阻断；R2/R5 为 warning 级仅告警 |
| FE-DEADCODE | 未使用导出 / 未使用依赖 | 合并 | P0-7 `npm run dead-code`（knip） | 发现即阻断；误报写进 `knip.json` 并注明理由 |
| FE-VULN | 已知依赖漏洞（high / critical） | 合并 | P1-1 `npm audit --audit-level=high` | 发现即阻断；无修复版本时登记带期限的例外（写明风险、期限与处置人） |
| FE-DUP | 前端代码克隆 | 审查 | P2-1 `npm run dup-check`（jscpd） | 仅告警；克隆检测无法可靠判定"不同写法的相同业务含义"，是否收敛由审查决定 |

**已知需要收敛的一点**：前端 `jscpd`（仅告警）与后端 `dupl`（阻断）是两条语义相同的克隆检测，阻断策略不同。当前保留现状，降级需显式决策并留记录，不得静默放宽。

**需要人工判断、不进自动门禁的**：疑似语义重复的归属判定、是否应抽象、模块职责是否合理。由 review checklist 覆盖，AI 审查只提供候选与证据。

## 门禁自身验证

`.harness/constraints/ci/gate.sh selftest` 验证门禁本身：失败退出码是否记为失败、注入的违规样例（console.log / 硬编码色值 / shared→staff 反向依赖）是否被检出、以及"被跳过"在本地与 `GATE_STRICT=1` 下是否分别为告警与失败。夹具为临时文件，退出时自动清理。
