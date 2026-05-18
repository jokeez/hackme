#!/usr/bin/env bash
set -euo pipefail

# ultimate_max_gate.sh
# Aggregated "maximum practical" gate for canonical production.
#
# Usage:
#   ADMIN_TOKEN=... COORD_ADMIN_TOKEN=... \
#   BASE=http://127.0.0.1:18080 COORD=http://127.0.0.1:18081 \
#   bash scripts/ops/ultimate_max_gate.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
source "$ROOT_DIR/scripts/lib/gate_common.sh"
gate_require_cmd "ultimate-max" bash
gate_require_cmd "ultimate-max" jq
gate_require_cmd "ultimate-max" curl

BASE="${BASE:-http://127.0.0.1:18080}"
COORD="${COORD:-http://127.0.0.1:18081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-${ADMIN_TOKEN}}"

RUN_ID="${RUN_ID:-ultimate_max_gate_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/gates/$RUN_ID}"
mkdir -p "$OUT_DIR"
RESULTS_JSONL="$OUT_DIR/results.jsonl"
gate_init_results_file "$RESULTS_JSONL"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[ultimate-max] ADMIN_TOKEN is required" >&2
  exit 2
fi
if [[ -z "$COORD_ADMIN_TOKEN" ]]; then
  echo "[ultimate-max] COORD_ADMIN_TOKEN is required" >&2
  exit 2
fi


echo "[ultimate-max] RUN_ID=${RUN_ID}"
echo "[ultimate-max] BASE=${BASE} COORD=${COORD}"

gate_run_case "ultimate-max" "$RESULTS_JSONL" "$OUT_DIR" "go-test-all" "full Go test suite" "1" \
  go test ./... -count=1

gate_run_case "ultimate-max" "$RESULTS_JSONL" "$OUT_DIR" "code-quality-audit" "static duplicate/drift quality audit" "1" \
  bash scripts/ops/code_quality_audit.sh

gate_run_case "ultimate-max" "$RESULTS_JSONL" "$OUT_DIR" "final-release-oneclick" "aggregated practical release gate" "1" \
  env ADMIN_TOKEN="$ADMIN_TOKEN" LOCAL_BASE="$BASE" VPS_BASE="$BASE" COORD_URL="$COORD" \
    RUN_PREDEPLOY_GATE=0 RUN_FINAL_PREFLIGHT=0 \
    bash scripts/ops/final_release_oneclick.sh

gate_run_case "ultimate-max" "$RESULTS_JSONL" "$OUT_DIR" "fuzz-super-gate" "fuzz + policy + p2p contracts" "1" \
  env ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$BASE" COORD="$COORD" STRICT_MODE=0 HARD_FAIL_PRIVATE_STAGE=0 \
    bash scripts/ops/fuzz_super_gate.sh

gate_run_case "ultimate-max" "$RESULTS_JSONL" "$OUT_DIR" "redteam-hard-mode" "defensive adversarial hard suite" "1" \
  env ADMIN_TOKEN="$ADMIN_TOKEN" COORD_ADMIN_TOKEN="$COORD_ADMIN_TOKEN" BASE="$BASE" COORD="$COORD" \
    bash scripts/ops/redteam_hard_mode.sh

gate_run_case "ultimate-max" "$RESULTS_JSONL" "$OUT_DIR" "top-pool-canary" "operational readiness canary thresholds" "1" \
  env PROFILE=canary BASE="$BASE" COORD="$COORD" \
    bash scripts/ops/top_pool_readiness_gate.sh

# Informational: top-profile may fail before fleet scale-up.
gate_run_case "ultimate-max" "$RESULTS_JSONL" "$OUT_DIR" "top-pool-top" "top-scale target thresholds (informational until scale-up)" "0" \
  env PROFILE=top BASE="$BASE" COORD="$COORD" \
    bash scripts/ops/top_pool_readiness_gate.sh

total="$(wc -l <"$RESULTS_JSONL" | tr -d ' ')"
fails_required="$(jq -r 'select(.verdict=="fail" and .required==true) | .id' "$RESULTS_JSONL" | wc -l | tr -d ' ')"
fails_total="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS_JSONL" | wc -l | tr -d ' ')"

status="PASS"
if [[ "$fails_required" != "0" ]]; then
  status="FAIL"
elif [[ "$fails_total" != "0" ]]; then
  status="PASS_WITH_SCALE_GAPS"
fi

jq -nc \
  --arg gate "ultimate_max_gate_v1" \
  --arg run_id "$RUN_ID" \
  --arg base "$BASE" \
  --arg coord "$COORD" \
  --arg captured_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg status "$status" \
  --argjson total "$total" \
  --argjson fails_total "$fails_total" \
  --argjson fails_required "$fails_required" \
  '{
    gate:$gate,
    run_id:$run_id,
    base:$base,
    coord:$coord,
    captured_at:$captured_at,
    status:$status,
    total_checks:$total,
    failed_checks_total:$fails_total,
    failed_required_checks:$fails_required
  }' >"$OUT_DIR/summary.json"

echo "[ultimate-max] summary: $OUT_DIR/summary.json"
if [[ "$status" == "FAIL" ]]; then
  echo "[ultimate-max] FAIL (required checks failed)"
  exit 1
fi
echo "[ultimate-max] ${status}"
