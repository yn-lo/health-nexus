#!/usr/bin/env pwsh
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Write-Host "=== Health Nexus Frontend Gate ===" -ForegroundColor Cyan
$steps = @(
  @{ Name = 'ESLint'; Cmd = 'npx eslint src/ --quiet' },
  @{ Name = 'TypeCheck'; Cmd = 'npx vue-tsc --noEmit' },
  @{ Name = 'ArchTest'; Cmd = 'npx vitest run tests/arch/' },
  @{ Name = 'StyleGuard'; Cmd = 'node scripts/style-guard.mjs' }
)
$failed = @()
foreach ($s in $steps) {
  Write-Host "`n>> $($s.Name)" -ForegroundColor Yellow
  Invoke-Expression $s.Cmd
  if ($LASTEXITCODE -ne 0) { $failed += $s.Name }
}
if ($failed.Count -gt 0) {
  Write-Host "`nGATE FAILED: $($failed -join ', ')" -ForegroundColor Red
  exit 1
}
Write-Host "`nAll checks passed" -ForegroundColor Green
