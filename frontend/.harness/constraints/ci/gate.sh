#!/usr/bin/env bash
# Harness CI 门禁（前端版，Bash 版，Linux/macOS/CI）
# 规范：../harness.md（§5.2 单一门禁入口与双平台一致性、§5.3 结果状态与失败策略、§6.1 门禁自测）。
# 本脚本是前端唯一门禁实现；frontend/gate.sh 与 frontend/gate.ps1 只是调用它的薄包装。
# 分层：P0 静态分析 + 类型 + 测试 + 构建 + 死代码；P1 安全；P2 工程债 + 卫生。
# 约束输出原则：只输出错误，全部通过时输出一行确认。
# 优雅降级：可选工具未安装时跳过并告警，不阻塞；设 GATE_STRICT=1 后改为阻断
# （CI/发布流水线必须设，否则「没有运行」会被记成「通过」）。
#
# 用法：
#   .harness/constraints/ci/gate.sh           # 默认全跑 P0+P1+P2
#   .harness/constraints/ci/gate.sh p0        # 仅跑 P0
#   .harness/constraints/ci/gate.sh p1        # 仅跑 P1
#   .harness/constraints/ci/gate.sh p2        # 仅跑 P2
#   .harness/constraints/ci/gate.sh selftest  # 门禁自身验证（注入违规样例，必须被检出）
#   GATE_STRICT=1 .harness/constraints/ci/gate.sh   # CI 模式：缺工具/被跳过即失败
set -uo pipefail

# Windows PowerShell 兼容：从 PowerShell 调用时 stdout fd 可能被劫持导致内建 echo 写入失败。
if [ -t 0 ] && ! [ -t 1 ]; then
  exec 1>/dev/tty 2>/dev/tty
fi

# 切换到 frontend 目录（ci → constraints → .harness → frontend，共上溯三级）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$FRONTEND_ROOT"

TIER="${1:-all}"
TIER="$(echo "$TIER" | tr '[:upper:]' '[:lower:]')"
FAILURES=0
WARNINGS=0

has_tool() { command -v "$1" >/dev/null 2>&1; }

fail() {
  echo "✗ $1"
  [ -n "${2:-}" ] && echo "  FIX: $2"
  [ -n "${3:-}" ] && echo "  See: $3"
  FAILURES=$((FAILURES + 1))
}

warn() {
  echo "⚠ $1"
  WARNINGS=$((WARNINGS + 1))
}

# CI/发布模式：GATE_STRICT=1 时，「被跳过」视为失败——没有运行的检查不得记成通过。
STRICT="${GATE_STRICT:-}"

# skipped 记录一条被跳过的检查：严格模式失败，本地降级为告警。
skipped() {
  if [ -n "$STRICT" ]; then
    fail "$1" "${2:-在 CI 环境安装齐全部工具后重试}"
  else
    warn "$1"
  fi
}

# require_tool 检查必需工具；缺失时按 skipped 处理并返回 1，调用方据此跳过检查。
require_tool() {
  has_tool "$1" && return 0
  skipped "$1 不可用，已跳过 $2" "${3:-安装 $1}"
  return 1
}

# capture_fail 跑一条命令，失败则记录并把输出摘要写入失败条目。
capture_fail() {
  local name="$1" fix="$2" see="$3"; shift 3
  shift  # 跳过 "--"
  local out rc
  out="$("$@" 2>&1)"
  rc=$?
  if [ $rc -ne 0 ]; then
    fail "$name" "$fix" "$see"
    echo "$out" | grep -v -E '^(\s*$)' | head -n 30 | sed 's/^/    /'
  fi
}

# ============================================================================
# P0 — 静态分析 + 类型 + 测试 + 构建 + 死代码（阻塞主干）
# ============================================================================
run_p0() {
  echo "==> P0: 静态分析 / 类型 / 测试 / 构建"

  # P0-1 eslint
  capture_fail \
    "eslint 失败" \
    "修复报告的错误（no-console/no-unused-vars 等）" \
    "eslint.config.js" \
    -- npm run lint --silent

  # P0-2 vue-tsc 类型检查（strict）
  capture_fail \
    "type-check 失败（vue-tsc）" \
    "修复类型错误" \
    "tsconfig.json" \
    -- npm run type-check --silent

  # P0-3 架构约束测试（AC-ARCH-FE-* 20 条规则）
  capture_fail \
    "架构约束测试失败（AC-ARCH-FE-* 违规）" \
    "按违规信息解除反向/跨层依赖" \
    "tests/arch/governance.test.ts, .harness/specs/architecture/boundaries.md" \
    -- npm run test:arch --silent

  # P0-4 全量单元测试
  capture_fail \
    "单元测试失败" \
    "修复失败用例或补回归测试" \
    "tests/unit/" \
    -- npm test --silent

  # P0-5 构建（vue-tsc + vite build）
  capture_fail \
    "构建失败（vite build）" \
    "修复构建错误" \
    "vite.config.ts" \
    -- npm run build --silent

  # P0-6 样式约束（style-guard R1-R5）
  capture_fail \
    "样式约束失败（style-guard）" \
    "按报告修复样式违规" \
    "scripts/style-guard.mjs, .harness/specs/conventions/styling.md" \
    -- npm run lint:style --silent

  # P0-7 死代码/未使用依赖（knip，经 npm script 调用以兼容 Windows 路径解析）
  if require_tool npm "P0-7 死代码检查（knip）" "安装 Node.js/npm 并执行 npm install"; then
    capture_fail \
      "死代码检查失败（knip：未使用导出/依赖）" \
      "删除未使用导出或将误报加入 knip.json 配置" \
      "knip.json" \
      -- npm run dead-code --silent
  fi
}

# ============================================================================
# P1 — 安全（阻塞主干）
# ============================================================================
run_p1() {
  echo "==> P1: 安全"

  # P1-1 npm audit（依赖漏洞；audit-level=high 阻断）
  if require_tool npm "P1-1 依赖漏洞扫描（npm audit）" "安装 Node.js/npm 并执行 npm install"; then
    capture_fail \
      "npm audit 发现 high/critical 漏洞" \
      "运行 npm audit fix 或升级受影响依赖" \
      "https://github.com/advisories" \
      -- npm audit --audit-level=high
  fi
}

# ============================================================================
# P2 — 工程债 + 卫生（非阻塞主干，建议修）
# ============================================================================
run_p2() {
  echo "==> P2: 工程债 / 卫生"

  # P2-1 代码克隆检测（jscpd）
  # 策略：疑似语义重复属「需判断事项」，只告警供 review 参考，不阻断——与后端 gate.sh 一致。
  require_tool npm "P2-1 代码克隆检测（jscpd）" "安装 Node.js/npm 并执行 npm install" || return 0
  local dup_out dup_rc n
  dup_out="$(npm run dup-check --silent 2>&1)"; dup_rc=$?
  n="$(printf '%s\n' "$dup_out" | grep -oE 'Found [0-9]+ clones' | grep -oE '[0-9]+' | head -n1)"
  if [ -n "$n" ] && [ "$n" -gt 0 ]; then
    warn "jscpd 发现 ${n} 个代码克隆（P2 工程债，建议提取公共组件/工具）"
    printf '%s\n' "$dup_out" | grep -E 'Clone found|Found [0-9]+ clones' | head -n 5 | sed 's/^/    /'
  elif [ -z "$n" ]; then
    # 退出码非 0 且解析不出克隆数 = jscpd 自身没跑成（配置/依赖问题），
    # 不能像 `|| true` 那样静默通过，否则「没有运行」会被记成「通过」。
    skipped "jscpd 未产出结果（退出码 ${dup_rc}），P2-1 未真正执行" "检查 frontend/.jscpd.json 与依赖安装"
  fi
}

# ============================================================================
# selftest — 门禁自身验证（注入违规样例，必须被检出且最终返回失败）
# 自检不接受降级：探针工具不可用即失败，否则「自检通过」本身会变成假绿灯。
# ============================================================================
run_selftest() {
  echo "==> selftest: 门禁自身验证"

  probe_tool() {
    has_tool "$1" && return 0
    fail "selftest 无法验证：$1 不可用" "安装 $1 后重跑；自检不接受降级"
    return 1
  }

  # 探针路径不用 local：EXIT trap 在函数返回后才执行，local 变量此时已出作用域，
  # cleanup 会拿到空值而删不掉故意违规的样例文件。
  probe_lint='src/__gate_selftest_probe__.ts'
  probe_arch='src/shared/__gate_selftest_probe__.ts'
  cleanup() { rm -f "$probe_lint" "$probe_arch"; }
  trap cleanup EXIT

  # S1 失败管道探针：子命令失败必须被记为门禁失败（防止 `|| true` 类假绿灯复活）
  local before="$FAILURES"
  { capture_fail "probe" "" "" -- false ; } >/dev/null 2>&1
  if [ "$FAILURES" -eq "$before" ]; then
    fail "selftest: 失败退出码未被记为门禁失败" "检查 gate.sh 的 capture_fail 与管道写法"
  fi
  FAILURES="$before"

  probe_tool npx || return 1

  # S2 eslint 探针：注入 console.log（AC-ARCH-FE-10 / no-console）
  # 先捕获输出再断言：不能写成 `cmd | grep -q`——pipefail 下管道状态取检查器的失败码，会误判为「未检出」。
  local lint_rc lint_out
  printf 'const gateSelftestProbe = 1\nconsole.log(gateSelftestProbe)\n' > "$probe_lint"
  lint_out="$(npx eslint "$probe_lint" --quiet 2>&1)"; lint_rc=$?
  if [ "$lint_rc" -eq 0 ] || ! printf '%s\n' "$lint_out" | grep -q '__gate_selftest_probe__'; then
    fail "selftest: eslint 未检出注入的 console.log（退出码 ${lint_rc}）" \
      "检查 eslint.config.js 的 no-console 规则与 npm run lint 的扫描范围"
  fi

  # S3 style-guard 探针：注入硬编码色值（R1）
  local sg_rc sg_out
  printf 'export const gateSelftestProbe = "#ff0000"\n' > "$probe_lint"
  sg_out="$(node scripts/style-guard.mjs 2>&1)"; sg_rc=$?
  if [ "$sg_rc" -eq 0 ] || ! printf '%s\n' "$sg_out" | grep -q '__gate_selftest_probe__'; then
    fail "selftest: style-guard 未检出注入的硬编码色值（退出码 ${sg_rc}）" \
      "检查 scripts/style-guard.mjs 的 R1 规则与扫描范围"
  fi
  rm -f "$probe_lint"

  # S4 架构测试探针：注入 shared → staff 反向依赖（AC-ARCH-FE-22）
  printf "import type { X } from '@/staff/router'\nexport type GateSelftestProbe = X\n" > "$probe_arch"
  if npx vitest run tests/arch/ -t 'AC-ARCH-FE-22' >/dev/null 2>&1; then
    fail "selftest: 架构测试未检出 shared → staff 反向依赖" "检查 tests/arch/governance.test.ts 的 AC-ARCH-FE-22"
  fi
}

# ============================================================================
# 主流程
# ============================================================================
case "$TIER" in
  p0) run_p0 ;;
  p1) run_p1 ;;
  p2) run_p2 ;;
  selftest) run_selftest ;;
  all)
    run_p0
    run_p1
    run_p2
    ;;
  *)
    echo "用法: $0 [p0|p1|p2|selftest|all]" >&2
    exit 2
    ;;
esac

echo ""
if [ "$FAILURES" -gt 0 ]; then
  echo "✗ Harness 门禁失败：$FAILURES 项失败，$WARNINGS 项告警"
  exit 1
fi
if [ "$WARNINGS" -gt 0 ]; then
  echo "⚠ Harness 门禁通过（含 $WARNINGS 项非阻塞告警）"
  exit 0
fi
echo "All checks passed"
exit 0
