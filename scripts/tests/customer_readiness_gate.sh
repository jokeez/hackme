#!/usr/bin/env bash
# One-shot “ready for paying customers?” gate (safe tier, no destructive ops).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-$ROOT/reports/customer-readiness-$STAMP}"
mkdir -p "$OUT"
LOG="$OUT/run.log"
VERDICT="$OUT/VERDICT.md"

pass_n=0
fail_n=0

run_step() {
  local id="$1"
  shift
  echo "[readiness] === $id ===" | tee -a "$LOG"
  if "$@" >>"$LOG" 2>&1; then
    pass_n=$((pass_n + 1))
    echo "pass $id" >>"$OUT/steps.txt"
    return 0
  fi
  fail_n=$((fail_n + 1))
  echo "fail $id" >>"$OUT/steps.txt"
  return 1
}

run_step economics_confidence bash "$ROOT/scripts/tests/economics_confidence_gate.sh" || true
run_step security_audit bash "$ROOT/scripts/ops/security_audit_gate.sh" || true
run_step pool_fuzz_distributed bash "$ROOT/scripts/ops/pool_fuzz_distributed_gate.sh" || true
run_step pool_fuzz_sync bash "$ROOT/scripts/tests/pool_fuzz_sync_gate.sh" || true
run_step orders_multilang env ADMIN_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token" 2>/dev/null || true)" \
  bash "$ROOT/scripts/tests/orders_multilang_audit.sh" || true
run_step customer_phasing_local env PHASE_FROM=3 POLL_SEC=180 RATE_SLEEP=3 BUDGET_RUNS=12 POOL_DISTRIBUTED=false \
  bash "$ROOT/scripts/tests/customer_real_network_phasing.sh" || true

{
  echo "# Customer readiness — $STAMP"
  echo ""
  echo "- **PASS steps:** $pass_n"
  echo "- **FAIL steps:** $fail_n"
  echo ""
  if [[ "$fail_n" -eq 0 ]]; then
    echo "## Verdict: **GO** (production-grade audit + pool + economics)"
  else
    echo "## Verdict: **WARN** — fix failed steps in \`$OUT/run.log\`"
  fi
} >"$VERDICT"

echo "[readiness] → $VERDICT (pass=$pass_n fail=$fail_n)"
[[ "$fail_n" -eq 0 ]]
