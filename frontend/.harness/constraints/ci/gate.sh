#!/usr/bin/env bash
# Harness CI 门禁（前端版，Bash 版，Linux/macOS/CI）
# 分层：P0 静态分析 + 类型 + 测试 + 构建 + 死代码；P1 安全；P2 工程债 + 卫生。
# 约束输出原则：只输出错误，全部通过时输出一行确认。
# 优雅降级：可选工具未安装时跳过并告警，不阻塞。
#
# 用法：
#   .harness/constraints/ci/gate.sh           # 默认全跑 P0+P1+P2
#   .harness/constraints/ci/gate.sh p0        # 仅跑 P0
#   .harness/constraints/ci/gate.sh p1        # 仅跑 P1
#   .harness/constraints/ci/gate.sh p2        # 仅跑 P2
set -uo pipefail

# Windows PowerShell 兼容：从 PowerShell 调用时 stdout fd 可能被劫持导致内建 echo 写入失败。
if [ -t 0 ] && ! [ -t 1 ]; then
  exec 1>/dev/tty 2>/dev/tty
fi

# 切换到 frontend 目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
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
  if has_tool npm; then
    capture_fail \
      "死代码检查失败（knip：未使用导出/依赖）" \
      "删除未使用导出或将误报加入 knip.json 配置" \
      "knip.json" \
      -- npm run dead-code --silent
  else
    warn "npm 不可用，已跳过 P0-7 死代码检查"
  fi
}

# ============================================================================
# P1 — 安全（阻塞主干）
# ============================================================================
run_p1() {
  echo "==> P1: 安全"

  # P1-1 npm audit（依赖漏洞；audit-level=high 阻断）
  if has_tool npm; then
    capture_fail \
      "npm audit 发现 high/critical 漏洞" \
      "运行 npm audit fix 或升级受影响依赖" \
      "https://github.com/advisories" \
      -- npm audit --audit-level=high
  else
    warn "npm 不可用，已跳过 P1-1 依赖漏洞扫描"
  fi
}

# ============================================================================
# P2 — 工程债 + 卫生（非阻塞主干，建议修）
# ============================================================================
run_p2() {
  echo "==> P2: 工程债 / 卫生"

  # P2-1 代码克隆检测（jscpd；存在克隆即告警，不阻塞）
  local dup_out n
  dup_out="$(npm run dup-check --silent 2>&1 || true)"
  n="$(echo "$dup_out" | grep -oE 'Found [0-9]+ clones' | grep -oE '[0-9]+' | head -n1)"
  if [ -n "$n" ] && [ "$n" -gt 0 ]; then
    warn "jscpd 发现 ${n} 个代码克隆（P2 工程债，建议提取公共组件/工具）"
    echo "$dup_out" | grep -E 'Clone found|Found [0-9]+ clones' | head -n 5 | sed 's/^/    /'
  fi
}

# ============================================================================
# 主流程
# ============================================================================
case "$TIER" in
  p0) run_p0 ;;
  p1) run_p1 ;;
  p2) run_p2 ;;
  all)
    run_p0
    run_p1
    run_p2
    ;;
  *)
    echo "用法: $0 [p0|p1|p2|all]" >&2
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
