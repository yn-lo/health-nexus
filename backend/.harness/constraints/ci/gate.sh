#!/usr/bin/env bash
# Harness CI 门禁（Bash 版，Linux/macOS/CI）
# 参见 harness.md §4.6 门禁分层：P0 架构不变量 + 测试质量 + 静态分析；P1 安全 + 规范；P2 契约 + 工程债 + 卫生。
# 约束输出原则（harness.md §4.3）：只输出错误，全部通过时输出一行确认。
# 优雅降级（harness.md §1.4）：可选工具未安装时跳过并告警，不阻塞。
#
# 用法：
#   .harness/constraints/ci/gate.sh           # 默认全跑 P0+P1+P2
#   .harness/constraints/ci/gate.sh p0         # 仅跑 P0
#   .harness/constraints/ci/gate.sh p1         # 仅跑 P1
#   .harness/constraints/ci/gate.sh p2         # 仅跑 P2
set -uo pipefail

# Windows PowerShell 兼容：从 PowerShell 调用时 stdout fd 可能被劫持导致内建 echo 写入失败。
# 重新打开 stdout/stderr 到控制台，确保 echo/printf 正常工作。
if [ -t 0 ] && ! [ -t 1 ]; then
  exec 1>/dev/tty 2>/dev/tty
fi

# 切换到仓库根（go.mod 所在目录）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$REPO_ROOT"

TIER="${1:-all}"
TIER="$(echo "$TIER" | tr '[:upper:]' '[:lower:]')"
FAILURES=0
WARNINGS=0

# has_tool 判断工具是否可用。
has_tool() { command -v "$1" >/dev/null 2>&1; }

# fail 记录一条门禁失败。
fail() {
  echo "✗ $1"
  [ -n "${2:-}" ] && echo "  FIX: $2"
  [ -n "${3:-}" ] && echo "  See: $3"
  FAILURES=$((FAILURES + 1))
}

# warn 记录一条降级告警（不阻塞）。
warn() {
  echo "⚠ $1"
  WARNINGS=$((WARNINGS + 1))
}

# capture_fail 跑一条命令，失败则记录并把 stderr 摘要写入失败条目。
# 用法: capture_fail "<检查名>" "<修复建议>" "<文档引用>" -- <cmd...>
capture_fail() {
  local name="$1" fix="$2" see="$3"; shift 3
  shift  # 跳过 "--"
  local out rc
  out="$("$@" 2>&1)"
  rc=$?
  if [ $rc -ne 0 ]; then
    fail "$name" "$fix" "$see"
    # 输出工具自身错误（只保留错误原则的例外：让 AI 看到 why）
    # 过滤：通过项、空行、go build cache 路径噪音（长串哈希路径）
    echo "$out" | grep -v -E '^(ok|PASS|\s*$)' | \
      grep -v -E '(golangci-lint[/\\][0-9a-f]{2}[/\\][0-9a-f]+-[ad]$|Sandbox Error|hit restricted|Hint: You can configure)' | \
      head -n 40 | sed 's/^/    /'
  fi
}

# ============================================================================
# P0 — 架构不变量 + 测试质量 + 静态分析（阻塞主干）
# ============================================================================
run_p0() {
  echo "==> P0: 架构不变量 / 测试质量 / 静态分析"

  # P0-1 构建必须通过
  capture_fail \
    "go build ./... 失败" \
    "修复编译错误后重试" \
    "CLAUDE.md" \
    -- go build ./...

  # P0-2 go vet
  capture_fail \
    "go vet ./... 失败" \
    "修复 vet 报告的可疑构造" \
    ".golangci.yml govet" \
    -- go vet ./...

  # P0-3 架构约束测试（AC-ARCH-* 规则，分层依赖方向）
  # 分层合规检测：handler 不依赖 repository、service 不 import net/http、
  #               platform 不依赖 domain、shared 是叶子层、禁止跨域 import 等
  capture_fail \
    "架构约束测试失败（AC-ARCH-* 违规）" \
    "按违规信息解除反向/跨域依赖" \
    "internal/harness/arch/arch_test.go, .harness/specs/architecture/boundaries.md" \
    -- go test ./internal/harness/arch/ -count=1

  # P0-4 API 契约测试（70 端点路由完整性 + 鉴权门禁）
  capture_fail \
    "API 契约测试失败" \
    "新增/修改端点须同步更新 tests/api_contract_test.go 端点表" \
    "tests/api_contract_test.go, .harness/specs/conventions/testing.md" \
    -- go test ./tests/ -run 'TestRouteIntegrity|TestAuthGate|TestRoleGate' -count=1

  # P0-4b API 路由地图生成（chi.Walk 遍历路由树 + runtime 反射 handler 限定名 → docs/api-contract.md）
  # 每次门禁运行都重新生成，确保路由地图始终与代码同步，前端据此锁定 handler 源码位置。
  capture_fail \
    "API 路由地图生成失败" \
    "检查 tests/api_contract_gen_test.go 与路由树是否一致" \
    "tests/api_contract_gen_test.go, docs/api-contract.md" \
    -- go test ./tests/ -run 'TestGenerateAPIContract' -count=1

  # P0-5 单元测试（无 race）
  capture_fail \
    "单元测试失败" \
    "修复失败用例或补回归测试" \
    ".harness/specs/conventions/testing.md" \
    -- go test ./internal/... -count=1

  # P0-6 race 检测（需 cgo；无 cgo 时优雅降级为 warn）
  if [ -z "${GATE_SKIP_RACE:-}" ]; then
    if [ -z "${CGO_ENABLED:-}" ]; then
      # 未显式设置 CGO_ENABLED，探测默认值
      cgo_default="$(go env CGO_ENABLED 2>/dev/null || echo '0')"
    else
      cgo_default="$CGO_ENABLED"
    fi
    if [ "$cgo_default" = "1" ] || [ -n "${CGO_ENABLED:-}" ] && [ "${CGO_ENABLED:-}" = "1" ]; then
      capture_fail \
        "race 检测失败（数据竞争）" \
        "加锁或改用 channel 同步；参见 race 报告栈" \
        ".harness/specs/conventions/testing.md" \
        -- go test -race ./internal/... -count=1
    else
      # 尝试启用 cgo 跑一次（CI 服务器通常有 gcc）
      if command -v gcc >/dev/null 2>&1; then
        capture_fail \
          "race 检测失败（数据竞争）" \
          "加锁或改用 channel 同步；参见 race 报告栈" \
          ".harness/specs/conventions/testing.md" \
          -- env CGO_ENABLED=1 go test -race ./internal/... -count=1
      else
        warn "race 检测已跳过：CGO_ENABLED=0 且未找到 gcc（go test -race 需要 cgo；CI 服务器应安装 gcc 并设置 CGO_ENABLED=1）"
      fi
    fi
  fi

  # P0-7 golangci-lint（含 AC-ARCH-06/08/10 + AC-SEC 安全规则）
  # 检测项：死代码（unused/staticcheck U1000）、魔法值（mnd）、
  #         错误未处理（errcheck）、过长函数（funlen）、安全漏洞（gosec）、
  #         禁用依赖（depguard）、import 排序（goimports）等
  if has_tool golangci-lint; then
    # golangci-lint 的真实报告在 stdout，缓存/进度信息在 stderr。
    # 单独捕获：丢弃 stderr 噪音，只看 stdout 的真实 lint 报告。
    local lint_out lint_rc
    lint_out="$(golangci-lint run ./... 2>/dev/null)"
    lint_rc=$?
    if [ $lint_rc -ne 0 ]; then
      fail "golangci-lint 失败（含死代码/魔法值/安全规则）" \
        "按报告修复；常见：错误未处理(errcheck)、死代码(U1000/unused)、魔法值(mnd)、过长函数(funlen)" \
        ".golangci.yml"
      echo "$lint_out" | grep -v -E '^\s*$' | head -n 40 | sed 's/^/    /'
    fi
  else
    warn "golangci-lint 未安装，已跳过 P0-7 lint（CI 服务器应装齐；本地可 go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest）"
  fi

  # P0-7b 全程序可达性死代码扫描（golang.org/x/tools/cmd/deadcode）
  # 与 P0-7 的 unused/staticcheck U1000 互补：unused 按构建上下文分析，
  # deadcode 从 main/init 做整包可达性分析，能抓「仅被未用导出间接引用」的死函数。
  if has_tool deadcode; then
    capture_fail \
      "deadcode 发现不可达的死代码" \
      "删除 deadcode 报告中的未使用符号（非测试产物）" \
      "golang.org/x/tools/cmd/deadcode" \
      -- deadcode ./...
  else
    warn "deadcode 未安装，已跳过 P0-7b 全程序死代码扫描（go install golang.org/x/tools/cmd/deadcode@latest；CI 服务器应装齐）"
  fi

  # P0-8 数据库迁移完整性（goose 命名规范 + 编号唯一连续）
  if [ -d migrations ]; then
    local mig_bad=0
    # 1) 命名规范：NNNNN_name.sql（goose 约定，Bug#1 历史教训）
    local naming
    naming="$(ls migrations/*.sql 2>/dev/null | xargs -n1 basename 2>/dev/null | grep -vE '^[0-9]{5}_[a-z0-9_]+\.sql$' || true)"
    if [ -n "$naming" ]; then
      fail "迁移文件命名不符合 goose 规范（NNNNN_name.sql）" "重命名迁移文件" "migrations/, .harness/specs/e2e-test-plan.md Bug#1"
      echo "$naming" | head -n 10 | sed 's/^/    /'
      mig_bad=1
    fi
    # 2) 编号唯一（无重复）
    local dups
    dups="$(ls migrations/*.sql 2>/dev/null | xargs -n1 basename 2>/dev/null | grep -oE '^[0-9]{5}' | sort | uniq -d)"
    if [ -n "$dups" ]; then
      fail "迁移编号重复：$(echo "$dups" | tr '\n' ' ')" "按序重排迁移编号" "migrations/"
      mig_bad=1
    fi
    # 3) 编号连续（从 00001 开始无跳号）
    if [ "$mig_bad" -eq 0 ]; then
      local expected=1 num
      for f in $(ls migrations/*.sql 2>/dev/null | xargs -n1 basename 2>/dev/null | sort); do
        num="$((10#${f%%_*}))"
        if [ "$num" -ne "$expected" ]; then
          fail "迁移编号不连续：期望 0000${expected}，实际 ${num}" "检查缺失/跳号的迁移文件" "migrations/"
          break
        fi
        expected=$((expected + 1))
      done
    fi
  fi
}

# ============================================================================
# P1 — 安全 + 规范（阻塞主干）
# ============================================================================
run_p1() {
  echo "==> P1: 安全 / 规范"

  # P1-1 govulncheck（依赖漏洞扫描）
  if has_tool govulncheck; then
    capture_fail \
      "govulncheck 发现已知漏洞" \
      "升级受影响依赖到修复版本" \
      "https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck" \
      -- govulncheck ./...
  else
    warn "govulncheck 未安装，已跳过 P1-1 漏洞扫描（go install golang.org/x/vuln/cmd/govulncheck@latest）"
  fi

  # P1-2 gofmt 强制（不允许未格式化代码入库）
  local fmt_out
  fmt_out="$(gofmt -l . 2>/dev/null | grep -v -E '^(vendor/|\.git/)' || true)"
  if [ -n "$fmt_out" ]; then
    fail "gofmt 发现未格式化文件" "运行 gofmt -w ." "CLAUDE.md"
    echo "$fmt_out" | head -n 20 | sed 's/^/    /'
  fi

  # P1-3 goimports 强制（import 分组与排序）
  if has_tool goimports; then
    local imp_out
    imp_out="$(goimports -l . 2>/dev/null | grep -v -E '^(vendor/|\.git/)' || true)"
    if [ -n "$imp_out" ]; then
      fail "goimports 发现未整理的 import" "运行 goimports -w ." ".golangci.yml goimports"
      echo "$imp_out" | head -n 20 | sed 's/^/    /'
    fi
  fi
  # goimports 未安装时不重复告警——golangci-lint 的 goimports 规则已覆盖

  # P1-4 错误码登记完整性（新增业务错误必须在 error-codes.md 登记）
  if [ -f .harness/specs/reference/error-codes.md ]; then
    : # 占位：错误码↔代码一致性检查留作后续 ratchet，当前由 review checklist 覆盖
  fi

  # P1-5 Playwright E2E（前后端联调；服务未启动时优雅降级为告警，不阻塞）
  if has_tool npx; then
    local fe_alive be_alive
    # curl -w '%{http_code}' 连接失败时输出 000，工具不可用时为空 → 兜底 000
    fe_alive="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 http://localhost:5173/ 2>/dev/null)"
    fe_alive="${fe_alive:-000}"
    be_alive="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 http://localhost:5230/healthz 2>/dev/null)"
    be_alive="${be_alive:-000}"
    if [ "$fe_alive" != "000" ] && [ "$be_alive" != "000" ]; then
      capture_fail \
        "Playwright E2E 失败" \
        "修复 E2E 用例或前后端问题" \
        "frontend/tests/e2e/, frontend/playwright.config.ts" \
        -- bash -c 'cd ../frontend && npx playwright test --reporter=line'
    else
      warn "Playwright E2E 已跳过：前后端服务未启动（前端 ${fe_alive} / 后端 ${be_alive}；需先启动 air + vite dev）"
    fi
  fi
}

# ============================================================================
# P2 — 契约 + 工程债 + 卫生（非阻塞主干，建议修）
# ============================================================================
run_p2() {
  echo "==> P2: 契约 / 工程债 / 卫生"

  # P2-1 覆盖率 floor（service 层 >= 60%，ratchet，见 testing.md）
  if has_tool go; then
    local cov_out cov_rc
    cov_out="$(go test ./internal/... -cover -count=1 2>&1)"
    cov_rc=$?
    if [ $cov_rc -ne 0 ]; then
      # 测试失败已在 P0-5 捕获，这里不重复报错
      :
    else
      # 提取 service 层覆盖率，低于 floor 告警（P2 不阻塞）。
      # 注意：必须收集后在主 shell 统一 warn，管道子 shell 内 warn 不会更新计数。
      local low_list=""
      local line pct
      while IFS= read -r line; do
        pct="$(echo "$line" | grep -oE '[0-9]+\.[0-9]+%' | head -n1 | tr -d '%')"
        if [ -n "$pct" ]; then
          if awk "BEGIN {exit !($pct < 60)}"; then
            # go test -cover 行格式：ok <pkg> <time> coverage: xx.x% ...
            # $1 是 "ok"，包路径在 $2
            low_list="${low_list}$(echo "$line" | awk '{print $2}')\n"
          fi
        fi
      done <<< "$(echo "$cov_out" | grep -E 'domain/.*/service')"
      if [ -n "$low_list" ]; then
        while IFS= read -r pkg; do
          [ -n "$pkg" ] && warn "service 层覆盖率低于 floor 60%：$pkg"
        done <<< "$(printf '%b' "$low_list")"
      fi
    fi
  fi

  # P2-2 禁 TODO/FIXME 进生产代码（卫生）
  local todo_out
  todo_out="$(grep -rn -E '(TODO|FIXME|XXX|HACK)' --include='*.go' \
    --exclude-dir=testdata --exclude-dir=vendor --exclude-dir=.git \
    internal/ cmd/ 2>/dev/null | grep -v -E 'ponytail:|_test\.go' || true)"
  if [ -n "$todo_out" ]; then
    warn "生产代码中存在 TODO/FIXME（P2 卫生，建议清理或转工单）"
    echo "$todo_out" | head -n 10 | sed 's/^/    /'
  fi

  # P2-3 禁外网 CDN / 硬编码内网绕过（卫生，仅扫描非测试 go 文件）
  local cdn_out
  cdn_out="$(grep -rn -E 'https?://(cdn\.|unpkg\.|cdnjs\.)' --include='*.go' \
    --exclude-dir=testdata --exclude-dir=vendor internal/ cmd/ 2>/dev/null || true)"
  if [ -n "$cdn_out" ]; then
    fail "生产代码引用外网 CDN（P2 卫生）" "改为本地资源或经审核的白名单域名" "CLAUDE.md"
    echo "$cdn_out" | head -n 10 | sed 's/^/    /'
  fi

  # P2-4 Doc Freshness（设计文档是否过期，harness.md §4.6）
  if [ -d .harness/specs/design ]; then
    local stale_days=60 stale_files
    if [ "$(uname)" = "Darwin" ]; then
      stale_files="$(find .harness/specs/design -name '*.md' -mtime +$stale_days 2>/dev/null || true)"
    else
      stale_files="$(find .harness/specs/design -name '*.md' -mtime +$stale_days 2>/dev/null || true)"
    fi
    if [ -n "$stale_files" ]; then
      warn "设计文档超过 ${stale_days} 天未更新，可能已与实现脱节（P2 卫生）"
      echo "$stale_files" | head -n 10 | sed 's/^/    /'
    fi
  fi

  # P2-5 git 卫生：禁止敏感文件入库（.env / 密钥 / 本地配置）
  if has_tool git; then
    local sensitive
    sensitive="$(git ls-files 2>/dev/null | grep -E '(^|/)(\.env[a-z0-9._-]*|.*\.pem|.*\.key|config\.local\.yaml|.*\.p12|.*\.pfx)$' || true)"
    if [ -n "$sensitive" ]; then
      fail "git 追踪了敏感文件（.env/密钥/本地配置）" "从索引移除（git rm --cached）并确保在 .gitignore" ".gitignore"
      echo "$sensitive" | head -n 10 | sed 's/^/    /'
    fi
    # P2-6 git 卫生：大文件（>5MB）入库警告
    local big
    big="$(git ls-files 2>/dev/null | while IFS= read -r f; do
      local s
      s="$(git cat-file -s "HEAD:$f" 2>/dev/null || echo 0)"
      if [ -n "$s" ] && [ "$s" -gt 5242880 ] 2>/dev/null; then
        echo "$f ($s bytes)"
      fi
    done)"
    if [ -n "$big" ]; then
      warn "git 追踪了大文件（>5MB），建议改存对象存储或压缩"
      echo "$big" | head -n 10 | sed 's/^/    /'
    fi
  fi

  # P2-7 前端重复代码率门禁（jscpd，含 ts/js/vue；阈值与忽略规则见 frontend/.jscpd.json）
  # 重复率超阈值时 jscpd 退出非 0，与后端 dupl 同为阻塞项。
  # node_modules 未安装时优雅降级为告警（与 P1-5 一致），避免 backend-only 检出被卡。
  if [ -d ../frontend/node_modules ] && has_tool npm; then
    capture_fail \
      "前端重复代码率超阈值（jscpd）" \
      "按 consoleFull 报告重构重复代码；阈值 8%、minTokens 50，见 frontend/.jscpd.json" \
      "frontend/.jscpd.json" \
      -- bash -c 'cd ../frontend && npm run dup-check'
  else
    warn "前端 dup-check 已跳过：../frontend/node_modules 未安装或 npm 不可用（npm install 后生效）"
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
