#!/usr/bin/env bash
set -euo pipefail
echo "=== Health Nexus Frontend Gate ==="
failed=()
run() { echo ""; echo ">> $1"; shift; if ! "$@"; then failed+=("$1"); fi }
run "ESLint" npx eslint src/ --quiet
run "TypeCheck" npx vue-tsc --noEmit
run "ArchTest" npx vitest run tests/arch/
run "StyleGuard" node scripts/style-guard.mjs
if [ ${#failed[@]} -gt 0 ]; then
  echo ""; echo "GATE FAILED: ${failed[*]}"
  exit 1
fi
echo ""; echo "All checks passed"
