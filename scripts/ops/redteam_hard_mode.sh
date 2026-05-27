#!/usr/bin/env bash
set -euo pipefail

# redteam_hard_mode.sh
# One-command defensive red-team suite (non-malicious).
#
# Usage:
#   ADMIN_TOKEN=... COORD_ADMIN_TOKEN=... \
#   BASE=http://127.0.0.1:18080 COORD=http://127.0.0.1:18081 \
#   bash scripts/ops/redteam_hard_mode.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
source "$ROOT_DIR/scripts/lib/gate_common.sh"
gate_require_cmd "redteam-hard" bash
gate_require_cmd "redteam-hard" curl
gate_require_cmd "redteam-hard" jq

BASE="${BASE:-http://127.0.0.1:18080}"
COORD="${COORD:-http://127.0.0.1:18081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-${ADMIN_TOKEN}}"
P2P_TOKEN="${P2P_TOKEN:-${ADMIN_TOKEN}}"
RUN_ID="${RUN_ID:-redteam_hard_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/gates/$RUN_ID}"
mkdir -p "$OUT_DIR"
RESULTS="$OUT_DIR/results.jsonl"
gate_init_results_file "$RESULTS"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[redteam-hard] ADMIN_TOKEN is required" >&2
  exit 2
fi
if [[ -z "$COORD_ADMIN_TOKEN" ]]; then
  echo "[redteam-hard] COORD_ADMIN_TOKEN is required" >&2
  exit 2
fi


echo "[redteam-hard] RUN_ID=${RUN_ID}"
echo "[redteam-hard] BASE=${BASE} COORD=${COORD}"

gate_run_case "redteam-hard" "$RESULTS" "$OUT_DIR" "redteam-surface-smoke" "unauthorized mutating endpoints are rejected" "" \
  env BASE="$BASE" bash scripts/tests/redteam_surface_smoke.sh

gate_run_case "redteam-hard" "$RESULTS" "$OUT_DIR" "adversarial-api-matrix" "adversarial API matrix passes" "" \
  env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" P2P_TOKEN="$P2P_TOKEN" bash scripts/tests/adversarial_api_matrix.sh

gate_run_case "redteam-hard" "$RESULTS" "$OUT_DIR" "language-chaos-security" "language chaos security gate passes" "" \
  env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" bash scripts/tests/language_chaos_security.sh

gate_run_case "redteam-hard" "$RESULTS" "$OUT_DIR" "language-break-attempts" "language break attempts are rejected safely" "" \
  env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" bash scripts/tests/language_break_attempts.sh

gate_run_case "redteam-hard" "$RESULTS" "$OUT_DIR" "p2p-storm-harness" "p2p storm keeps node alive under load" "" \
  env RUN_ID="$RUN_ID" BASE="$BASE" P2P_TOKEN="$P2P_TOKEN" CONCURRENCY=120 REQUESTS=2400 MODE=mixed bash scripts/tests/p2p_storm_harness.sh

gate_run_case "redteam-hard" "$RESULTS" "$OUT_DIR" "p2p-smoke" "p2p contract smoke passes" "" \
  env RUN_ID="$RUN_ID" BASE="$BASE" P2P_TOKEN="$P2P_TOKEN" bash scripts/tests/p2p_smoke.sh

gate_run_case "redteam-hard" "$RESULTS" "$OUT_DIR" "coordinator-matrix" "coordinator abuse + dedup matrix passes" "" \
  env RUN_ID="$RUN_ID" COORD="$COORD" BASE="$BASE" COORD_ADMIN_TOKEN="$COORD_ADMIN_TOKEN" bash scripts/tests/coordinator_matrix.sh

gate_run_case "redteam-hard" "$RESULTS" "$OUT_DIR" "hybrid-signer-smoke" "strict hybrid signer rejects unsigned submits" "" \
  env COORD_URL="$COORD" COORD_TOKEN="$ADMIN_TOKEN" REQUIRE_HYBRID=1 bash scripts/tests/hybrid_signer_smoke.sh

gate_run_case "redteam-hard" "$RESULTS" "$OUT_DIR" "sup-accrual-gate" "SUP honest accrual policy unit tests pass" "" \
  env COORD_URL="$COORD" bash scripts/ops/sup_accrual_gate.sh

gate_run_case "redteam-hard" "$RESULTS" "$OUT_DIR" "difficulty-health" "difficulty retarget health within policy bounds" "" \
  env RUN_ID="$RUN_ID" BASE="$BASE" bash scripts/tests/difficulty_health.sh

gate_run_case "redteam-hard" "$RESULTS" "$OUT_DIR" "chain-invariants" "chain economics invariants hold" "" \
  env BASE="$BASE" bash scripts/check_invariants.sh

dup_log="$OUT_DIR/dup-chain-check.log"
if chain_json="$(curl -fsS --max-time 20 "${BASE}/api/chain?limit=400")"; then
  printf '%s' "$chain_json" | jq -r '{
    count:(.blocks|length),
    unique_index:(.blocks|map(.index)|unique|length),
    unique_hash:(.blocks|map(.hash)|unique|length),
    max_index:(.blocks|map(.index)|max)
  }' >"$OUT_DIR/dup_chain_check.json"
  cat "$OUT_DIR/dup_chain_check.json" >"$dup_log"
  count="$(jq -r '.count // 0' "$OUT_DIR/dup_chain_check.json")"
  uidx="$(jq -r '.unique_index // 0' "$OUT_DIR/dup_chain_check.json")"
  uhash="$(jq -r '.unique_hash // 0' "$OUT_DIR/dup_chain_check.json")"
  if [[ "$count" == "$uidx" && "$count" == "$uhash" ]]; then
    echo "[redteam-hard] PASS duplicate-chain-check"
    gate_record_result_jsonl "$RESULTS" "duplicate-chain-check" "pass" "no duplicate block indices/hashes in recent window" "$dup_log"
  else
    echo "[redteam-hard] FAIL duplicate-chain-check (see ${dup_log})"
    gate_record_result_jsonl "$RESULTS" "duplicate-chain-check" "fail" "duplicate index/hash detected in recent chain window" "$dup_log"
  fi
else
  echo "[redteam-hard] FAIL duplicate-chain-check (chain endpoint unavailable)"
  gate_record_result_jsonl "$RESULTS" "duplicate-chain-check" "fail" "failed to fetch /api/chain for duplicate check" "$dup_log"
fi

total="$(wc -l <"$RESULTS" | tr -d ' ')"
fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"

jq -nc \
  --arg gate "redteam_hard_mode_v1" \
  --arg run_id "$RUN_ID" \
  --arg base "$BASE" \
  --arg coord "$COORD" \
  --arg captured_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson total "$total" \
  --argjson fails "$fails" \
  '{gate:$gate,run_id:$run_id,base:$base,coord:$coord,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' \
  >"$OUT_DIR/summary.json"

echo "[redteam-hard] summary: $OUT_DIR/summary.json"
if [[ "$fails" != "0" ]]; then
  echo "[redteam-hard] FAIL ($fails/$total)"
  exit 1
fi
echo "[redteam-hard] PASS ($total checks)"
