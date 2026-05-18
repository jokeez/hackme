#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-public_release_mega_$(run_id)}"
OUT="$OUT_DIR/$RID/public_release_mega"
ensure_reports_dir "$OUT"
RESULTS="$OUT/results.jsonl"
: >"$RESULTS"

# Quick profile by default; tune up for real pre-release.
ULTIMATE_MEGA_DURATION="${ULTIMATE_MEGA_DURATION:-120}"
ULTIMATE_PRE_DURATION="${ULTIMATE_PRE_DURATION:-180}"
ULTIMATE_PRE_INTERVAL="${ULTIMATE_PRE_INTERVAL:-30}"
P2P_STORM_REQUESTS="${P2P_STORM_REQUESTS:-2000}"
P2P_STORM_CONCURRENCY="${P2P_STORM_CONCURRENCY:-120}"
P2P_STORM_MODE="${P2P_STORM_MODE:-mixed}"
MAX_WORKER_DOMINANCE_PCT="${MAX_WORKER_DOMINANCE_PCT:-51}"
FAST_PROFILE="${FAST_PROFILE:-0}"
ULTIMATE_TIMEOUT_SEC="${ULTIMATE_TIMEOUT_SEC:-1800}"

record_case() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
}

run_case() {
  local id="$1"
  local detail="$2"
  shift 2
  local log_file="$OUT/${id}.log"
  if "$@" >"$log_file" 2>&1; then
    record_case "$id" "pass" "$detail"
  else
    record_case "$id" "fail" "$detail (see $log_file)"
  fi
}

run_case "health-status" "status endpoint responds" curl -fsS "$BASE/api/status"
run_case "health-metrics" "metrics endpoint responds" curl -fsS "$BASE/api/metrics"
run_case "health-global" "global metrics endpoint responds" curl -fsS "$BASE/api/global/metrics"

if [[ "$FAST_PROFILE" == "1" ]]; then
  run_case "ultimate-quick" "fast-profile ultimate validation bundle" \
    env RUN_ID="${RID}_ultimate" BASE="$BASE" COORD="$COORD" \
        UNIT_TESTS=0 RUN_REHEARSAL=0 \
        SKIP_DAILY_FULL=1 SKIP_MEGA_STRESS=1 SKIP_PRE_RELEASE=1 SKIP_ADV_MATRIX_BUNDLE=1 \
        CURL_MAX_TIME=6 BURST_REQUESTS=12 CLAIM_RATE_PROBE_ATTEMPTS=12 CLAIM_RATE_PROBE_FAIL_FAST_000=2 \
        "$ROOT_DIR/scripts/tests/run_ultimate_validation.sh"
else
  if command -v timeout >/dev/null 2>&1; then
    run_case "ultimate-quick" "shortened ultimate validation bundle (timeout-guarded)" \
      timeout "${ULTIMATE_TIMEOUT_SEC}" env RUN_ID="${RID}_ultimate" BASE="$BASE" COORD="$COORD" \
        UNIT_TESTS=0 RUN_REHEARSAL=0 \
        MEGA_DURATION_SEC="$ULTIMATE_MEGA_DURATION" \
        PRE_DURATION_SEC="$ULTIMATE_PRE_DURATION" \
        PRE_INTERVAL_SEC="$ULTIMATE_PRE_INTERVAL" \
        "$ROOT_DIR/scripts/tests/run_ultimate_validation.sh"
  else
    run_case "ultimate-quick" "shortened ultimate validation bundle" \
      env RUN_ID="${RID}_ultimate" BASE="$BASE" COORD="$COORD" \
          UNIT_TESTS=0 RUN_REHEARSAL=0 \
          MEGA_DURATION_SEC="$ULTIMATE_MEGA_DURATION" \
          PRE_DURATION_SEC="$ULTIMATE_PRE_DURATION" \
          PRE_INTERVAL_SEC="$ULTIMATE_PRE_INTERVAL" \
          "$ROOT_DIR/scripts/tests/run_ultimate_validation.sh"
  fi
fi

if [[ -n "${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}" ]]; then
  run_case "p2p-storm" "p2p noisy-client storm harness" \
    env RUN_ID="${RID}_p2p" BASE="$BASE" P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}" \
        REQUESTS="$P2P_STORM_REQUESTS" CONCURRENCY="$P2P_STORM_CONCURRENCY" MODE="$P2P_STORM_MODE" \
        "$ROOT_DIR/scripts/tests/p2p_storm_harness.sh"
else
  record_case "p2p-storm" "pass" "skipped: P2P_TOKEN/HACKME_P2P_TOKEN not set"
fi

run_case "security-assertions" "security invariants and tx abuse smoke" \
  env RUN_ID="${RID}_security" BASE="$BASE" "$ROOT_DIR/scripts/tests/security_assertions.sh"

# 51%-style risk diagnostic:
# detect whether a single worker dominates accepted attempts beyond threshold.
dominance_log="$OUT/worker_dominance.log"
if curl -fsS "$COORD/api/work/stats" >"$dominance_log" 2>/dev/null; then
  top_share="$(jq -r '
    (.workers // {}) as $w
    | [ $w[]?.accepted_attempts // 0 ] as $arr
    | ($arr | add // 0) as $sum
    | ($arr | max // 0) as $mx
    | if $sum > 0 then (100.0 * $mx / $sum) else 0 end
  ' "$dominance_log")"
  if jq -en --arg v "$top_share" --arg max "$MAX_WORKER_DOMINANCE_PCT" '($v|tonumber) > ($max|tonumber)' >/dev/null; then
    record_case "worker-dominance" "fail" "top worker dominance ${top_share}% > ${MAX_WORKER_DOMINANCE_PCT}%"
  else
    record_case "worker-dominance" "pass" "top worker dominance ${top_share}% <= ${MAX_WORKER_DOMINANCE_PCT}%"
  fi
else
  record_case "worker-dominance" "fail" "cannot fetch coordinator work stats from $COORD"
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"
total="$(wc -l <"$RESULTS" | tr -d ' ')"
jq -nc \
  --arg run_id "$RID" \
  --arg captured_at "$(ts_utc)" \
  --arg base "$BASE" \
  --arg coord "$COORD" \
  --argjson total "$total" \
  --argjson fails "$fails" \
  '{run_id:$run_id,captured_at:$captured_at,base:$base,coord:$coord,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' \
  >"$OUT/summary.json"

echo "Public release mega summary: $OUT/summary.json"
if [[ "$fails" != "0" ]]; then
  fail "public release mega FAIL ($fails/$total). See $OUT"
fi
pass "public release mega PASS ($total checks). See $OUT"

