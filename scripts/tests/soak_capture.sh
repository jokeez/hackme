#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

BASE="${BASE:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
DURATION_SEC="${DURATION_SEC:-3600}"
INTERVAL_SEC="${INTERVAL_SEC:-300}"
OUT="$OUT_DIR/$RID/soak"
ensure_reports_dir "$OUT"

start_ts="$(date +%s)"
end_ts=$((start_ts + DURATION_SEC))
idx=0

while [[ "$(date +%s)" -lt "$end_ts" ]]; do
  ts="$(ts_utc)"
  snap="$OUT/snapshot_${idx}.json"
  status_json="$(json_get "$BASE/api/status" || echo '{}')"
  metrics_json="$(json_get "$BASE/api/metrics" || echo '{}')"
  wallet_json="$(json_get "$BASE/api/wallet" || echo '{}')"
  jq -nc \
    --arg ts "$ts" \
    --argjson status "$status_json" \
    --argjson metrics "$metrics_json" \
    --argjson wallet "$wallet_json" \
    '{ts:$ts,status:$status,metrics:$metrics,wallet:$wallet}' >"$snap"
  idx=$((idx+1))
  sleep "$INTERVAL_SEC"
done

jq -nc --arg run_id "$RID" --arg base "$BASE" --arg captured_at "$(ts_utc)" --argjson snapshots "$idx" \
  --argjson duration "$DURATION_SEC" --argjson interval "$INTERVAL_SEC" \
  '{run_id:$run_id,base:$base,captured_at:$captured_at,snapshots:$snapshots,duration_sec:$duration,interval_sec:$interval,status:"DONE"}' >"$OUT/summary.json"

pass "soak capture completed: $OUT ($idx snapshots)"
