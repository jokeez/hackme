#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd jq
require_cmd go
require_cmd python3

BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-ultimate_$(run_id)}"
OUT="$OUT_DIR/$RID/ultimate"
ensure_reports_dir "$OUT"
RESULTS="$OUT/results.jsonl"
: >"$RESULTS"

# Core knobs
UNIT_TESTS="${UNIT_TESTS:-1}"
RUN_REHEARSAL="${RUN_REHEARSAL:-0}"
NODES="${NODES:-http://127.0.0.1:8080}"

# Mega stress knobs
MEGA_DURATION_SEC="${MEGA_DURATION_SEC:-1800}"
MEGA_TX_WORKERS="${MEGA_TX_WORKERS:-48}"
MEGA_ORDERS_WORKERS="${MEGA_ORDERS_WORKERS:-16}"
MEGA_COORD_WORKERS="${MEGA_COORD_WORKERS:-24}"
MEGA_SAMPLE_INTERVAL_SEC="${MEGA_SAMPLE_INTERVAL_SEC:-2}"
MEGA_ORDERS_MODE="${MEGA_ORDERS_MODE:-nospend}" # nospend | spend

# Pre-release soak knobs
PRE_DURATION_SEC="${PRE_DURATION_SEC:-3600}"
PRE_INTERVAL_SEC="${PRE_INTERVAL_SEC:-120}"

# After MODE=full, block (3) repeats transfers/orders/security/adversarial/coordinator with separate RUN_IDs.
# Set to 1 for long runs when full gate coverage is enough.
SKIP_ADV_MATRIX_BUNDLE="${SKIP_ADV_MATRIX_BUNDLE:-0}"
SKIP_DAILY_FULL="${SKIP_DAILY_FULL:-0}"
SKIP_MEGA_STRESS="${SKIP_MEGA_STRESS:-0}"
SKIP_PRE_RELEASE="${SKIP_PRE_RELEASE:-0}"

record_case() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
}

run_cmd_case() {
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

echo "== ultimate validation =="
echo "RUN_ID=$RID BASE=$BASE COORD=$COORD"

# 0) Health precheck
run_cmd_case "health-status" "status endpoint responds" \
  curl -fsS "$BASE/api/status"
run_cmd_case "health-metrics" "metrics endpoint responds" \
  curl -fsS "$BASE/api/metrics"

# 1) Full gate
if [[ "$SKIP_DAILY_FULL" == "1" ]]; then
  record_case "daily-full" "pass" "skipped by SKIP_DAILY_FULL=1"
else
  run_cmd_case "daily-full" "run_daily full gate" \
    env MODE=full RUN_ID="${RID}_full" BASE="$BASE" COORD="$COORD" "$ROOT_DIR/scripts/tests/run_daily.sh"
fi

# 2) Unit tests (optional)
if [[ "$UNIT_TESTS" == "1" ]]; then
  run_cmd_case "unit-chain-block" "go test internal chain/block" \
    env GOWORK=off go test ./internal/chain ./internal/block
else
  record_case "unit-chain-block" "pass" "skipped by UNIT_TESTS=0"
fi

# 3) Adversarial matrix bundle (duplicates much of MODE=full unless SKIP_ADV_MATRIX_BUNDLE=1)
if [[ "$SKIP_ADV_MATRIX_BUNDLE" == "1" ]]; then
  record_case "adv-bundle" "pass" "skipped: SKIP_ADV_MATRIX_BUNDLE=1 (already covered by daily-full)"
else
  run_cmd_case "adv-transfers" "transfers adversarial matrix" \
    env RUN_ID="${RID}_adv" BASE="$BASE" "$ROOT_DIR/scripts/tests/transfers_matrix.sh"
  run_cmd_case "adv-orders" "orders adversarial matrix" \
    env RUN_ID="${RID}_adv" BASE="$BASE" "$ROOT_DIR/scripts/tests/orders_matrix.sh"
  run_cmd_case "adv-security" "security assertions matrix" \
    env RUN_ID="${RID}_adv" BASE="$BASE" "$ROOT_DIR/scripts/tests/security_assertions.sh"
  run_cmd_case "adv-api-matrix" "adversarial API matrix" \
    env RUN_ID="${RID}_adv" BASE="$BASE" ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}" P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}" "$ROOT_DIR/scripts/tests/adversarial_api_matrix.sh"
  run_cmd_case "adv-coordinator" "coordinator matrix" \
    env RUN_ID="${RID}_adv" COORD="$COORD" "$ROOT_DIR/scripts/tests/coordinator_matrix.sh"
  run_cmd_case "adv-report" "adversarial summary generation" \
    env RUN_ID="${RID}_adv" "$ROOT_DIR/scripts/tests/report_summary.sh"
fi

# 4) Mega stress stage
if [[ "$SKIP_MEGA_STRESS" == "1" ]]; then
  record_case "mega-stress" "pass" "skipped by SKIP_MEGA_STRESS=1"
else
  run_cmd_case "mega-stress" "mega stress harness + post-security gate" \
    env RUN_ID="${RID}_mega" \
        BASE="$BASE" \
        COORD="$COORD" \
        PRECHECK_FULL=0 \
        POSTCHECK_SECURITY=1 \
        DURATION_SEC="$MEGA_DURATION_SEC" \
        TX_WORKERS="$MEGA_TX_WORKERS" \
        ORDERS_WORKERS="$MEGA_ORDERS_WORKERS" \
        COORD_WORKERS="$MEGA_COORD_WORKERS" \
        SAMPLE_INTERVAL_SEC="$MEGA_SAMPLE_INTERVAL_SEC" \
        ORDERS_MODE="$MEGA_ORDERS_MODE" \
        "$ROOT_DIR/scripts/tests/mega_stress.sh"
fi

# 5) Pre-release soak
if [[ "$SKIP_PRE_RELEASE" == "1" ]]; then
  record_case "pre-release" "pass" "skipped by SKIP_PRE_RELEASE=1"
else
  run_cmd_case "pre-release" "run_daily pre_release with soak" \
    env MODE=pre_release RUN_ID="${RID}_pre" BASE="$BASE" COORD="$COORD" \
        DURATION_SEC="$PRE_DURATION_SEC" INTERVAL_SEC="$PRE_INTERVAL_SEC" \
        "$ROOT_DIR/scripts/tests/run_daily.sh"
fi

# 6) Optional multi-node rehearsal
if [[ "$RUN_REHEARSAL" == "1" ]]; then
  run_cmd_case "rehearsal" "multi-node rehearsal_onboarding" \
    env RUN_ID="${RID}_rehearsal" NODES="$NODES" "$ROOT_DIR/scripts/tests/rehearsal_onboarding.sh"
else
  record_case "rehearsal" "pass" "skipped by RUN_REHEARSAL=0"
fi

# Aggregate ultimate summary
fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"
total="$(wc -l <"$RESULTS" | tr -d ' ')"
jq -nc \
  --arg run_id "$RID" \
  --arg captured_at "$(ts_utc)" \
  --arg base "$BASE" \
  --arg coord "$COORD" \
  --argjson total "$total" \
  --argjson fails "$fails" \
  '{
    run_id:$run_id,
    captured_at:$captured_at,
    base:$base,
    coord:$coord,
    total:$total,
    fails:$fails,
    status:(if $fails==0 then "PASS" else "FAIL" end)
  }' >"$OUT/summary.json"

RUN_ID="$RID" "$ROOT_DIR/scripts/tests/report_summary.sh" >"$OUT/report_summary.log" 2>&1 || true

echo "Ultimate summary: $OUT/summary.json"
if [[ "$fails" != "0" ]]; then
  fail "ultimate validation FAIL ($fails/$total). See $OUT"
fi
pass "ultimate validation PASS ($total checks). See $OUT"

