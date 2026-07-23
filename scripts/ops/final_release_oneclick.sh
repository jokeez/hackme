#!/usr/bin/env bash
set -euo pipefail

# final_release_oneclick.sh
# One-command final gate runner with consolidated PASS/FAIL summary.
#
# Usage (example):
#   ADMIN_TOKEN=... LOCAL_BASE=http://127.0.0.1:8080 VPS_BASE=http://<vps>:18080 COORD_URL=http://<vps>:18081 \
#   bash scripts/ops/final_release_oneclick.sh
#
# Optional toggles:
#   RUN_GO_TEST=1
#   RUN_PREDEPLOY_GATE=1
#   RUN_FINAL_PREFLIGHT=1
#   RUN_CORE_GATE=0
#   RUN_SETTLEMENT_HEALTH=1

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
source "$ROOT_DIR/scripts/lib/gate_common.sh"

LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
VPS_BASE="${VPS_BASE:-http://127.0.0.1:18080}"
COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
COORD="${COORD:-$COORD_URL}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"

RUN_GO_TEST="${RUN_GO_TEST:-1}"
RUN_PREDEPLOY_GATE="${RUN_PREDEPLOY_GATE:-1}"
RUN_FINAL_PREFLIGHT="${RUN_FINAL_PREFLIGHT:-0}"
RUN_CORE_GATE="${RUN_CORE_GATE:-0}"
RUN_SETTLEMENT_HEALTH="${RUN_SETTLEMENT_HEALTH:-1}"

REQUIRE_WALLET_SOURCE="${REQUIRE_WALLET_SOURCE:-1}"
RUN_HYBRID_SIGNER_SMOKE="${RUN_HYBRID_SIGNER_SMOKE:-1}"

RUN_ID="${RUN_ID:-final_release_oneclick_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/gates/$RUN_ID}"
mkdir -p "$OUT_DIR"
RESULTS_JSONL="$OUT_DIR/results.jsonl"
gate_init_results_file "$RESULTS_JSONL"
gate_require_cmd "final-release" bash
gate_require_cmd "final-release" jq
gate_require_cmd "final-release" curl

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[final-release] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 1
fi
if [[ "$ADMIN_TOKEN" == *"..."* || "$ADMIN_TOKEN" == *"CHANGE_ME"* || "$ADMIN_TOKEN" == *"PUT_FULL_TOKEN_HERE"* ]]; then
  echo "[final-release] ADMIN_TOKEN looks like placeholder; set real secret token" >&2
  exit 1
fi


echo "[final-release] RUN_ID=${RUN_ID}"
echo "[final-release] LOCAL_BASE=${LOCAL_BASE} VPS_BASE=${VPS_BASE} COORD_URL=${COORD_URL}"
echo "[final-release] OUT_DIR=${OUT_DIR}"

effective_require_wallet_source="$REQUIRE_WALLET_SOURCE"
if [[ "$LOCAL_BASE" == "$VPS_BASE" && "${REQUIRE_WALLET_SOURCE:-}" == "1" ]]; then
  # Single-node canonical run: wallet_source is expected to be local_db.
  effective_require_wallet_source="0"
fi
effective_run_predeploy="$RUN_PREDEPLOY_GATE"
if [[ "$LOCAL_BASE" == "$VPS_BASE" && "${RUN_PREDEPLOY_GATE:-}" == "1" ]]; then
  # predeploy_gate includes worker-mode health that expects local mining OFF.
  # In single-node canonical mode local mining is expected ON.
  effective_run_predeploy="0"
fi

gate_run_case "final-release" "$RESULTS_JSONL" "$OUT_DIR" "status-local" "local /api/status is reachable" "" \
  curl -fsS "${LOCAL_BASE}/api/status"
gate_run_case "final-release" "$RESULTS_JSONL" "$OUT_DIR" "status-vps" "vps /api/status is reachable" "" \
  curl -fsS "${VPS_BASE}/api/status"
gate_run_case "final-release" "$RESULTS_JSONL" "$OUT_DIR" "status-coordinator" "coordinator /api/work/stats is reachable" "" \
  curl -fsS "${COORD_URL}/api/work/stats"

if [[ "$RUN_GO_TEST" == "1" ]]; then
  gate_run_case "final-release" "$RESULTS_JSONL" "$OUT_DIR" "go-test-all" "go test ./... succeeds" "" \
    go test ./... -count=1
fi

if [[ "$RUN_SETTLEMENT_HEALTH" == "1" ]]; then
  gate_run_case "final-release" "$RESULTS_JSONL" "$OUT_DIR" "settlement-health" "worker settlement SLA/healthcheck passes" "" \
    env COORD_URL="$COORD_URL" LOCAL_BASE="$LOCAL_BASE" \
      bash scripts/ops/settlement_healthcheck.sh
fi

if [[ "$effective_run_predeploy" == "1" ]]; then
  gate_run_case "final-release" "$RESULTS_JSONL" "$OUT_DIR" "predeploy-gate" "predeploy gate passes" "" \
    env ADMIN_TOKEN="$ADMIN_TOKEN" LOCAL_BASE="$LOCAL_BASE" VPS_BASE="$VPS_BASE" COORD_URL="$COORD_URL" \
      REQUIRE_WALLET_SOURCE="$effective_require_wallet_source" RUN_CORE_GATE="$RUN_CORE_GATE" \
      RUN_HYBRID_SIGNER_SMOKE="$RUN_HYBRID_SIGNER_SMOKE" RUN_ID="${RUN_ID}_predeploy" \
      bash scripts/ops/predeploy_gate.sh
fi

if [[ "$RUN_FINAL_PREFLIGHT" == "1" ]]; then
  gate_run_case "final-release" "$RESULTS_JSONL" "$OUT_DIR" "final-preflight" "final preflight suite passes" "" \
    env ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$LOCAL_BASE" VPS_BASE="$VPS_BASE" COORD="$COORD" \
      RUN_ID="${RUN_ID}_preflight" OUT_DIR="$OUT_DIR" \
      bash scripts/ops/final_preflight.sh
fi

total="$(wc -l <"$RESULTS_JSONL" | tr -d ' ')"
fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS_JSONL" | wc -l | tr -d ' ')"

jq -nc \
  --arg run_id "$RUN_ID" \
  --arg local_base "$LOCAL_BASE" \
  --arg vps_base "$VPS_BASE" \
  --arg coord_url "$COORD_URL" \
  --arg out_dir "$OUT_DIR" \
  --arg captured_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson total "$total" \
  --argjson fails "$fails" \
  '{run_id:$run_id,local_base:$local_base,vps_base:$vps_base,coord_url:$coord_url,out_dir:$out_dir,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' \
  >"$OUT_DIR/summary.json"

echo "[final-release] summary: $OUT_DIR/summary.json"
if [[ "$fails" != "0" ]]; then
  echo "[final-release] FAIL ($fails/$total)"
  exit 1
fi
echo "[final-release] PASS ($total checks)"

