#!/usr/bin/env bash
# Post-exchange opt-in bytes pilot on prod coordinator (NOT auto-run).
# Requires explicit OPT_IN=1 and P1–P4 deployed on coordinator.
#
# Usage (after exchange rebuild):
#   OPT_IN=1 RUNS=256 BUDGET_HMC=5.0 bash scripts/ops/run_prod_bytes_pilot.sh
#
# Default: dry-run prints jq body only.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

if [[ "${OPT_IN:-0}" != "1" ]]; then
  echo "[prod-bytes-pilot] REFUSED: set OPT_IN=1 after P1–P4 prod deploy + migrate"
  echo "  See internal/poolfuzz/pilot_prod.go ProdOptInBytesPilotConfig"
  exit 2
fi

BASE="${BASE:-https://hackme.tech}"
COORD="${COORD:-${BASE}/pool/coordinator}"
ADMIN="$(tr -d '\r\n' < "${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}")"
CID="${CID:-bytes-pilot-$(date +%Y%m%d-%H%M%S)}"
RUNS="${RUNS:-256}"
BUDGET_HMC="${BUDGET_HMC:-5.0}"
MAX_INPUT="${MAX_INPUT:-1024}"
WASM="${WASM:-$ROOT/tasks/artifacts/security/rust_tracefuse_detector_bytes_guard.wasm}"
DRY_RUN="${DRY_RUN:-0}"

[[ -f "$WASM" ]] || { echo "missing WASM: $WASM" >&2; exit 1; }
WASM_HEX="$(xxd -p "$WASM" | tr -d '\n')"

create_body="$(jq -nc \
  --arg id "$CID" \
  --arg wasm "$WASM_HEX" \
  --argjson runs "$RUNS" \
  --argjson budget "$BUDGET_HMC" \
  --argjson max_input "$MAX_INPUT" \
  '{
    id: $id,
    campaign_type: "property",
    status: "running",
    title: "Opt-in bytes pilot (P4)",
    description: "guided_scheduling + input_mode=bytes — customer pilot only",
    budget_runs: $runs,
    budget_seconds: 7200,
    budget_hmc: $budget,
    config: {
      pool_distributed: true,
      check_semantics: "detector",
      wasm_check_hex: $wasm,
      input_mode: "bytes",
      max_input_bytes: $max_input,
      guided_scheduling: true,
      pool_corpus_max: 256,
      mutation_rounds: 6,
      queue_depth: 128,
      stable_crash_buckets: true,
      pilot: "bytes_v1",
      seed_byte_corpus: [
        "4157535f4143434553535f4b45595f49443d414b4941494f53464f444e4e374558414d504c45",
        "46524f4d206e6f64653a6c6174657374"
      ]
    }
  }')"

echo "[prod-bytes-pilot] campaign=$CID runs=$RUNS max_input_bytes=$MAX_INPUT"
if [[ "$DRY_RUN" == "1" ]]; then
  echo "$create_body" | jq .
  echo "[prod-bytes-pilot] DRY_RUN=1 — not posting"
  exit 0
fi

curl -fsS -X POST "$BASE/api/fuzz/campaigns" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -H "Content-Type: application/json" \
  -d "$create_body" | jq -c '{ok,id:.campaign.id,status:.campaign.status,report_token:.customer_report_token}'

echo "[prod-bytes-pilot] coordinator stats"
curl -fsS "$COORD/api/fuzz/pool/stats" | jq -c '{work_done,workers,queue_depth}'
