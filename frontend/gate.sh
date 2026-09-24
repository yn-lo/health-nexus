#!/usr/bin/env bash
# 前端门禁入口（薄包装）
#
# 唯一实现见 .harness/constraints/ci/gate.sh（P0+P1+P2 全量）。
# 本文件刻意不定义任何检查项，避免再次出现「多套范围不同的门禁」。
#
# 用法同唯一实现：
#   ./gate.sh              # 全跑 P0+P1+P2
#   ./gate.sh p0           # 仅 P0
#   ./gate.sh selftest     # 门禁自身验证
#   GATE_STRICT=1 ./gate.sh  # CI 模式：被跳过的检查视为失败
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
exec bash .harness/constraints/ci/gate.sh "$@"
