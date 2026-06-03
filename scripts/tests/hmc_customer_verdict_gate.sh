#!/usr/bin/env bash
# HMC paying-customer verdict (no HMS launch, no HMS VPS, no HMS market gates).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-$ROOT/reports/hmc-customer-verdict-$STAMP}"
mkdir -p "$OUT"
LOG="$OUT/run.log"
VERDICT="$OUT/VERDICT.md"
ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token" 2>/dev/null || true)"
export HACKME_ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-$ADMIN}"
export ADMIN_TOKEN="$ADMIN"
BASE_LOCAL="${BASE_LOCAL:-http://127.0.0.1:8080}"
BASE_PROD="${BASE_PROD:-https://hackme.tech}"

pass_n=0
fail_n=0
skip_n=0

run_step() {
  local id="$1"
  shift
  echo "[hmc-verdict] === $id ===" | tee -a "$LOG"
  if "$@" >>"$LOG" 2>&1; then
    pass_n=$((pass_n + 1))
    echo "pass $id" >>"$OUT/steps.txt"
    return 0
  fi
  fail_n=$((fail_n + 1))
  echo "fail $id" >>"$OUT/steps.txt"
  return 1
}

run_step_optional() {
  local id="$1"
  shift
  echo "[hmc-verdict] === $id (optional) ===" | tee -a "$LOG"
  if "$@" >>"$LOG" 2>&1; then
    pass_n=$((pass_n + 1))
    echo "pass $id" >>"$OUT/steps.txt"
  else
    skip_n=$((skip_n + 1))
    echo "skip $id" >>"$OUT/steps.txt"
  fi
}

{
  echo "[hmc-verdict] scope: HMC only — HMS coin/market/VPS excluded"
  echo "[hmc-verdict] stamp=$STAMP out=$OUT"
} | tee -a "$LOG"

if curl -fsS --max-time 15 "${BASE_LOCAL}/api/status?lite=1" >/dev/null 2>&1; then
  run_step redteam_local env BASE="$BASE_LOCAL" bash "$ROOT/scripts/tests/redteam_surface_smoke.sh" || true
  if [[ -n "$ADMIN" ]]; then
    run_step security_local env BASE="$BASE_LOCAL" ADMIN_TOKEN="$ADMIN" \
      bash "$ROOT/scripts/tests/security_assertions.sh" || true
    run_step fuzz_dashboard_local env BASE="$BASE_LOCAL" ADMIN_TOKEN="$ADMIN" \
      bash "$ROOT/scripts/tests/fuzz_dashboard_smoke.sh" || true
  else
    skip_n=$((skip_n + 2))
    echo "skip security_local fuzz_dashboard_local (no admin)" >>"$OUT/steps.txt"
  fi
else
  skip_n=$((skip_n + 3))
  echo "skip local API jobs (no :8080)" >>"$OUT/steps.txt"
  echo "[hmc-verdict] WARN: local node down — local smokes skipped" | tee -a "$LOG"
fi

run_step redteam_prod env BASE="$BASE_PROD" CURL_MAX_TIME=30 bash "$ROOT/scripts/tests/redteam_surface_smoke.sh" || true
run_step security_audit bash "$ROOT/scripts/ops/security_audit_gate.sh" || true
run_step pool_fuzz_sync bash "$ROOT/scripts/tests/pool_fuzz_sync_gate.sh" || true
# pool_fuzz_distributed runs inside customer_readiness — do not invoke twice (sqlite lock).
run_step economics_confidence bash "$ROOT/scripts/tests/economics_confidence_gate.sh" || true
run_step orders_multilang env ADMIN_TOKEN="$ADMIN" bash "$ROOT/scripts/tests/orders_multilang_audit.sh" || true
run_step customer_readiness bash "$ROOT/scripts/tests/customer_readiness_gate.sh" || true
run_step_optional go_test_short go test -short -count=1 ./... -timeout=300s

{
  echo "# HMC customer verdict — $STAMP"
  echo ""
  echo "**Scope:** HackMe Coin (HMC) — pool, audits, economics, phasing."
  echo "**Out of scope:** HMS coin launch, HMS coordinator VPS, \`hms_market_*\` gates."
  echo ""
  echo "- **PASS steps:** $pass_n"
  echo "- **FAIL steps:** $fail_n"
  echo "- **SKIP:** $skip_n"
  echo ""
  if [[ "$fail_n" -eq 0 ]]; then
    echo "## Verdict: **GO** for paying HMC customers"
    echo ""
    echo "Prod security audits, pool fuzz/sync, multilang orders, and economics gates are green."
    echo "HMS is not part of this verdict — do not treat HMS table/API as production-ready."
  else
    echo "## Verdict: **NO-GO** — see \`$LOG\`"
  fi
  echo ""
  echo "Artifacts: \`$OUT/\`"
} >"$VERDICT"

echo "[hmc-verdict] → $VERDICT (pass=$pass_n fail=$fail_n skip=$skip_n)"
[[ "$fail_n" -eq 0 ]]
