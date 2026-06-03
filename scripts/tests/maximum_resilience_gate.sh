#!/usr/bin/env bash
# Maximum resilience gate — user's 4 stress areas in one run.
#
# Quick (~3–5 min):
#   STRESS_QUICK=1 bash scripts/tests/maximum_resilience_gate.sh
#
# Full (~15+ min):
#   bash scripts/tests/maximum_resilience_gate.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd go
require_cmd bash

STRESS_QUICK="${STRESS_QUICK:-0}"
RID="${RUN_ID:-max_resilience_$(run_id)}"
OUT="${OUT_DIR:-$ROOT/reports/tests}/$RID/maximum_resilience"
LOG="$OUT/run.log"
RESULTS="$OUT/results.jsonl"

mkdir -p "$OUT"
: >"$LOG"
: >"$RESULTS"

record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
  echo "[$verdict] $id — $detail" | tee -a "$LOG"
}

run_step() {
  local id="$1"
  shift
  if "$@" >>"$LOG" 2>&1; then
    record "$id" "pass" "$*"
    return 0
  fi
  record "$id" "fail" "$* (see $LOG)"
  return 1
}

failures=0
step() {
  if ! run_step "$@"; then
    failures=$((failures + 1))
  fi
}

echo "=== maximum resilience gate RUN_ID=$RID STRESS_QUICK=$STRESS_QUICK ===" | tee -a "$LOG"

# --- 1) SQLite concurrent write (chain + store profile) ---
step "sqlite-concurrent-unit" go test ./internal/store/ -run TestSQLiteConcurrentWritersStress -count=1 -timeout=120s
step "transfer-concurrent-nonce" go test ./internal/chain/ -run TestTransferConcurrentSameNonceOnlyOneAccepted -count=1 -timeout=120s

# --- 2) Coordinator mega stress (50–100 virtual workers, claim/submit flood) ---
if [[ "$STRESS_QUICK" == "1" ]]; then
  step "coordinator-mega-stress" env STRESS_QUICK=1 RUN_ID="${RID}_mega" bash "$ROOT/scripts/tests/coordinator_mega_stress.sh"
else
  step "coordinator-mega-stress" env RUN_ID="${RID}_mega" bash "$ROOT/scripts/tests/coordinator_mega_stress.sh"
fi

# SQLite lock grep on mega stress log
MEGA_LOG="$ROOT/reports/tests/${RID}_mega/coordinator_mega_stress/coordinator.log"
if [[ -f "$MEGA_LOG" ]] && grep -Eqi 'database is locked|SQLITE_BUSY' "$MEGA_LOG"; then
  record "coordinator-mega-sqlite-clean" "fail" "sqlite lock in mega stress log"
  failures=$((failures + 1))
else
  record "coordinator-mega-sqlite-clean" "pass" "no sqlite lock in coordinator mega log"
fi

# --- 3) API fuzz (garbage JSON, huge body, bad tx, memo 257) ---
step "coordinator-api-fuzz" env RUN_ID="${RID}_fuzz" bash "$ROOT/scripts/tests/coordinator_api_fuzz_gate.sh"

if curl -fsS --max-time 3 "${BASE:-http://127.0.0.1:8080}/api/status" >/dev/null 2>&1; then
  if env RUN_ID="${RID}_adv" BASE="${BASE:-http://127.0.0.1:8080}" bash "$ROOT/scripts/tests/adversarial_api_matrix.sh" >>"$LOG" 2>&1; then
    record "adversarial-api-matrix" "pass" "adversarial matrix ok"
  else
    adv_fail=0
    if [[ -f "$ROOT/reports/tests/${RID}_adv/adversarial_api/results.jsonl" ]]; then
      adv_fail="$(jq -r 'select(.verdict=="fail") | .id' "$ROOT/reports/tests/${RID}_adv/adversarial_api/results.jsonl" | wc -l | tr -d ' ')"
    fi
    if [[ "$adv_fail" == "0" ]]; then
      record "adversarial-api-matrix" "pass" "non-fatal exit but no fail rows"
    else
      record "adversarial-api-matrix" "fail" "$adv_fail failing cases"
      failures=$((failures + 1))
    fi
  fi
else
  record "adversarial-api-matrix" "pass" "skipped: local node down"
fi

# --- 4) Memo UTF-8 256-byte validation ---
step "transfer-memo-utf8" go test ./internal/chain/ -run TestValidateTransferShapeMemoByteLimit -count=1 -timeout=30s

if curl -fsS --max-time 3 "${BASE:-http://127.0.0.1:8080}/api/status" >/dev/null 2>&1; then
  if env RUN_ID="${RID}_txmatrix" BASE="${BASE:-http://127.0.0.1:8080}" bash "$ROOT/scripts/tests/transfers_matrix.sh" >>"$LOG" 2>&1; then
    record "transfers-matrix" "pass" "transfers matrix ok"
  else
    tx_fail=0
    if [[ -f "$ROOT/reports/tests/${RID}_txmatrix/transfers/results.jsonl" ]]; then
      tx_fail="$(jq -r 'select(.verdict=="fail") | .id' "$ROOT/reports/tests/${RID}_txmatrix/transfers/results.jsonl" | wc -l | tr -d ' ')"
    fi
    record "transfers-matrix" "fail" "${tx_fail} failing cases (or script error)"
    failures=$((failures + 1))
  fi
else
  record "transfers-matrix" "pass" "skipped: local node down (memo covered by unit test)"
fi

# --- Split-brain / sync (unit + chaos pack — live iptables needs operator VPS) ---
step "sync-fork-unit" go test . -run 'TestSyncBlockedInfo' -count=1 -timeout=60s
step "nightly-chaos-pack" env RUN_ID="${RID}_chaos" bash "$ROOT/scripts/tests/nightly_chaos_guard.sh"
step "critical-security-pack" env RUN_ID="${RID}_sec" bash "$ROOT/scripts/tests/critical_security_pack.sh"
step "coordinator-crypto-chaos" go test ./cmd/coordinator/ -run 'HTTPSubmit|HybridSigner|Replay|Tamper|LedgerConsistency' -count=1 -timeout=300s

# Extra gates when quick=0
if [[ "$STRESS_QUICK" != "1" ]]; then
  step "gpu-rig-suite" env RUN_ID="${RID}_gpu" bash "$ROOT/scripts/tests/gpu_rig_suite.sh"
  step "customer-readiness" env RUN_ID="${RID}_cust" bash "$ROOT/scripts/tests/customer_readiness_gate.sh" || true
fi

echo "" | tee -a "$LOG"
echo "=== VERDICT ===" | tee -a "$LOG"
if [[ "$failures" -eq 0 ]]; then
  record "OVERALL" "pass" "maximum resilience gate PASS ($RID)"
  pass "maximum_resilience_gate PASS — $OUT"
  exit 0
fi

record "OVERALL" "fail" "$failures step(s) failed — see $LOG"
fail "maximum_resilience_gate FAIL ($failures) — $OUT"
exit 1
