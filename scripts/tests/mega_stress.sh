#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd python3
require_cmd jq
require_cmd curl

BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/mega_stress"
ensure_reports_dir "$OUT"

DURATION_SEC="${DURATION_SEC:-900}"
TX_WORKERS="${TX_WORKERS:-24}"
ORDERS_WORKERS="${ORDERS_WORKERS:-8}"
COORD_WORKERS="${COORD_WORKERS:-12}"
SAMPLE_INTERVAL_SEC="${SAMPLE_INTERVAL_SEC:-2}"
WORKER_DELAY_MS="${WORKER_DELAY_MS:-10}"
PRECHECK_FULL="${PRECHECK_FULL:-1}"
POSTCHECK_SECURITY="${POSTCHECK_SECURITY:-1}"
ORDERS_MODE="${ORDERS_MODE:-nospend}" # nospend | spend
PROFILE="${PROFILE:-mixed}" # mixed | tx-heavy | orders-heavy | coord-heavy
NETERR_TX_MAX="${NETERR_TX_MAX:-0.10}"
METRICS_ERROR_MAX_FACTOR="${METRICS_ERROR_MAX_FACTOR:-1}"

ADMIN_TOKEN="${ADMIN_TOKEN:-}"
COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-${ADMIN_TOKEN:-}}"

# Remote/VPS runs are far more likely to hit client-side socket saturation in burst harness.
# Keep strict defaults for localhost; relax only default thresholds for non-local BASE.
if [[ "$BASE" != http://127.0.0.1:* && "$BASE" != http://localhost:* ]]; then
  if [[ -z "${NETERR_TX_MAX_OVERRIDE:-}" ]]; then
    NETERR_TX_MAX="0.98"
  fi
  if [[ -z "${METRICS_ERROR_MAX_FACTOR_OVERRIDE:-}" ]]; then
    METRICS_ERROR_MAX_FACTOR="20"
  fi
fi

if [[ "$PRECHECK_FULL" == "1" ]]; then
  echo "== precheck full =="
  MODE=full RUN_ID="$RID" BASE="$BASE" COORD="$COORD" "$ROOT_DIR/scripts/tests/run_daily.sh"
fi

echo "== mega stress load =="
python3 "$ROOT_DIR/scripts/tests/tools/mega_stress_runner.py" \
  --base "$BASE" \
  --coord "$COORD" \
  --duration-sec "$DURATION_SEC" \
  --tx-workers "$TX_WORKERS" \
  --orders-workers "$ORDERS_WORKERS" \
  --coord-workers "$COORD_WORKERS" \
  --sample-interval-sec "$SAMPLE_INTERVAL_SEC" \
  --worker-delay-ms "$WORKER_DELAY_MS" \
  --orders-mode "$ORDERS_MODE" \
  --profile "$PROFILE" \
  --admin-token "$ADMIN_TOKEN" \
  --coord-admin-token "$COORD_ADMIN_TOKEN" \
  --output "$OUT/stress_report.json"

if [[ "$POSTCHECK_SECURITY" == "1" ]]; then
  echo "== postcheck security =="
  RUN_ID="$RID" BASE="$BASE" "$ROOT_DIR/scripts/tests/security_assertions.sh"
fi

# Compute gate decision with conservative thresholds.
ratio5_tx="$(jq -r '.scenarios.tx_burst.ratio_5xx // 0' "$OUT/stress_report.json")"
ratio5_orders="$(jq -r '.scenarios.orders_burst.ratio_5xx // 0' "$OUT/stress_report.json")"
ratio5_coord="$(jq -r '.scenarios.coordinator_claim_burst.ratio_5xx // 0' "$OUT/stress_report.json")"
neterr_tx="$(jq -r '.scenarios.tx_burst.ratio_network_error // 0' "$OUT/stress_report.json")"
metrics_samples="$(jq -r '.metrics.samples // 0' "$OUT/stress_report.json")"
metrics_errors="$(jq -r '.metrics.errors // 0' "$OUT/stress_report.json")"
min_hashrate="$(jq -r '.metrics.min_hashrate_th_s // -1' "$OUT/stress_report.json")"

fails=0
notes=()

if jq -en --arg v "$ratio5_tx" '($v|tonumber) > 0.05' >/dev/null; then
  fails=$((fails+1)); notes+=("tx_burst 5xx ratio too high: $ratio5_tx")
fi
if jq -en --arg v "$ratio5_orders" '($v|tonumber) > 0.10' >/dev/null; then
  fails=$((fails+1)); notes+=("orders_burst 5xx ratio too high: $ratio5_orders")
fi
if jq -en --arg v "$ratio5_coord" '($v|tonumber) > 0.20' >/dev/null; then
  fails=$((fails+1)); notes+=("coordinator_claim_burst 5xx ratio too high: $ratio5_coord")
fi
if jq -en --arg v "$neterr_tx" --arg max "$NETERR_TX_MAX" '($v|tonumber) > ($max|tonumber)' >/dev/null; then
  fails=$((fails+1)); notes+=("tx_burst network error ratio too high: $neterr_tx (max=$NETERR_TX_MAX)")
fi
if [[ "$metrics_samples" -lt 3 ]]; then
  fails=$((fails+1)); notes+=("too few metric samples: $metrics_samples")
fi
max_metric_errors="$(jq -nr --arg s "$metrics_samples" --arg f "$METRICS_ERROR_MAX_FACTOR" '((($s|tonumber) * ($f|tonumber))|floor)')"
if [[ "$metrics_errors" -gt "$max_metric_errors" ]]; then
  fails=$((fails+1)); notes+=("metrics errors exceed threshold: errors=$metrics_errors max=$max_metric_errors samples=$metrics_samples factor=$METRICS_ERROR_MAX_FACTOR")
fi
if [[ "${SKIP_MIN_HASHRATE_GATE:-0}" != "1" ]]; then
  if jq -en --arg v "$min_hashrate" '($v|tonumber) >= 0 and ($v|tonumber) < 0.01' >/dev/null; then
    fails=$((fails+1)); notes+=("min hashrate collapsed: $min_hashrate TH/s")
  fi
fi

notes_json="$(printf '%s\n' "${notes[@]:-}" | jq -R . | jq -s .)"
total_cases=7

jq -nc \
  --arg run_id "$RID" \
  --arg base "$BASE" \
  --arg coord "$COORD" \
  --arg captured_at "$(ts_utc)" \
  --argjson duration_sec "$DURATION_SEC" \
  --argjson tx_workers "$TX_WORKERS" \
  --argjson orders_workers "$ORDERS_WORKERS" \
  --argjson coord_workers "$COORD_WORKERS" \
  --arg profile "$PROFILE" \
  --argjson total "$total_cases" \
  --argjson fails "$fails" \
  --argjson notes "$notes_json" \
  '{
    run_id:$run_id,
    base:$base,
    coord:$coord,
    captured_at:$captured_at,
    duration_sec:$duration_sec,
    profile:$profile,
    workers:{tx:$tx_workers,orders:$orders_workers,coordinator:$coord_workers},
    total:$total,
    fails:$fails,
    status:(if $fails==0 then "PASS" else "FAIL" end),
    notes:$notes
  }' >"$OUT/summary.json"

RUN_ID="$RID" "$ROOT_DIR/scripts/tests/report_summary.sh"

if [[ "$fails" != "0" ]]; then
  fail "mega stress FAIL ($fails/$total_cases). See $OUT"
fi
pass "mega stress PASS ($total_cases checks). See $OUT"

