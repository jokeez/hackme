#!/usr/bin/env bash
# Nightly autonomous chaos guard: ledger, crypto replay/tamper, HackMe OS init.
#
# Usage:
#   bash scripts/tests/nightly_chaos_guard.sh          # one shot
#   CHAOS_LOOP=1 INTERVAL_SEC=3600 bash scripts/tests/nightly_chaos_guard.sh
#
# Optional live VPS probe (requires token + reachability):
#   CHAOS_LIVE_COORD_URL=https://hackme.tech/pool/coordinator \
#   CHAOS_LIVE_TOKEN=... bash scripts/tests/nightly_chaos_guard.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd go
require_cmd bash

OUT="${OUT_DIR:-$ROOT/reports/tests}/${RUN_ID:-$(run_id)}/nightly_chaos_guard"
ensure_reports_dir "$OUT"
LOG="$OUT/run.log"
: >"$LOG"

CHAOS_LOOP="${CHAOS_LOOP:-0}"
INTERVAL_SEC="${INTERVAL_SEC:-3600}"
MAX_ROUNDS="${MAX_ROUNDS:-0}"

run_step() {
  local name="$1"
  shift
  echo "=== $name ===" | tee -a "$LOG"
  if "$@" >>"$LOG" 2>&1; then
    pass "$name" | tee -a "$LOG"
    return 0
  fi
  fail "$name (see $LOG)" | tee -a "$LOG"
  return 1
}

one_round() {
  local round_id
  round_id="$(run_id)"
  local round_out="$OUT/round_${round_id}"
  mkdir -p "$round_out"

  (
    export OUT_DIR="$round_out"
    cd "$ROOT"

    run_step "poolledger_unit" go test ./internal/poolledger/... -count=1 -timeout=120s
    run_step "ledger_spec_chain" go test ./internal/chain/ -run 'Ledger|Conservation' -count=1 -timeout=180s
    run_step "coordinator_ledger_5000" go test ./cmd/coordinator/ -run 'TreasuryLedger5000|LedgerConsistency' -count=1 -timeout=300s
    run_step "coordinator_crypto_chaos" go test ./cmd/coordinator/ -run 'HTTPSubmit|HybridSigner|Replay|Tamper|submitReject' -count=1 -timeout=120s
    run_step "critical_security_pack" bash "$ROOT/scripts/tests/critical_security_pack.sh"
    run_step "hackme_os_init_worker" bash "$ROOT/scripts/release/iso/init_worker_test.sh"

    if [[ -n "${CHAOS_LIVE_COORD_URL:-}" && -n "${CHAOS_LIVE_TOKEN:-}" ]]; then
      run_step "live_coord_stats" curl -fsS --max-time 15 \
        -H "X-Hackme-Admin-Token: ${CHAOS_LIVE_TOKEN}" \
        "${CHAOS_LIVE_COORD_URL%/}/api/work/stats" \
        -o "$round_out/live_work_stats.json"
      jq -e '.target_mod > 0' "$round_out/live_work_stats.json" >>"$LOG" 2>&1 || fail "live coordinator missing target_mod"
    else
      echo "[skip] live VPS probe (set CHAOS_LIVE_COORD_URL + CHAOS_LIVE_TOKEN)" >>"$LOG"
    fi
  )

  echo "{\"round\":\"$round_id\",\"ts\":\"$(ts_utc)\"}" >>"$OUT/rounds.jsonl"
}

round=0
while true; do
  round=$((round + 1))
  echo "[chaos-guard] round=$round ts=$(ts_utc)" | tee -a "$LOG"
  if one_round; then
    echo "[chaos-guard] round $round PASS" | tee -a "$LOG"
  else
    echo "[chaos-guard] round $round FAIL — see $LOG" | tee -a "$LOG"
    exit 1
  fi
  if [[ "$CHAOS_LOOP" != "1" ]]; then
    break
  fi
  if [[ "$MAX_ROUNDS" -gt 0 && "$round" -ge "$MAX_ROUNDS" ]]; then
    break
  fi
  sleep "$INTERVAL_SEC"
done

pass "nightly_chaos_guard complete — $OUT"
echo "[chaos-guard] log=$LOG"
