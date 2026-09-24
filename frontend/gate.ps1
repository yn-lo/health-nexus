#!/usr/bin/env pwsh
# 前端门禁入口（薄包装）
#
# 唯一实现见 .harness/constraints/ci/gate.sh（P0+P1+P2 全量）。
# 本文件刻意不定义任何检查项，避免再次出现「多套范围不同的门禁」。
# 注意：本文件含中文，必须保存为带 BOM 的 UTF-8，否则 Windows PowerShell 5.1 会按 GBK 解码导致语法错误。
#
# 用法同唯一实现：
#   ./gate.ps1              # 全跑 P0+P1+P2
#   ./gate.ps1 p0           # 仅 P0
#   ./gate.ps1 selftest     # 门禁自身验证
#   $env:GATE_STRICT=1; ./gate.ps1   # CI 模式：被跳过的检查视为失败
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
# PowerShell 7.3+ 默认让原生命令非 0 退出抛异常，会掩盖 $LASTEXITCODE 的精确传播。
$PSNativeCommandUseErrorActionPreference = $false

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $root
try {
  if (-not (Get-Command bash -ErrorAction SilentlyContinue)) {
    Write-Host 'GATE FAILED: 未找到 bash，门禁未运行（Windows 请安装 Git Bash 并加入 PATH）' -ForegroundColor Red
    exit 1
  }
  bash .harness/constraints/ci/gate.sh @args
  exit $LASTEXITCODE
} finally {
  Pop-Location
}
