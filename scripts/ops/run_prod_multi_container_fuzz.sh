#!/usr/bin/env bash
# Multi-worker pool fuzz on production (simulates distinct containers).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

BASE="${BASE:-https://hackme.tech}"
COORD="${COORD:-${BASE}/pool/coordinator}"
ADMIN_FILE="${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}"
WORKER_FILE="${WORKER_TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_worker_token}"

if [[ -n "${NODE_SSH:-}" ]]; then
  rsync -az "$ROOT/scripts/ops/run_prod_multi_container_fuzz.sh" \
    "$ROOT/tasks/artifacts/security/rust_script_push_bounds_guard.wasm" \
    "$NODE_SSH:/opt/hackme/scripts/ops/" 2>/dev/null || true
  rsync -az "$ROOT/tasks/artifacts/security/rust_script_push_bounds_guard.wasm" \
    "$NODE_SSH:/opt/hackme/tasks/artifacts/security/" 2>/dev/null || true
  ssh -o BatchMode=yes "$NODE_SSH" \
    "cd /opt/hackme && NODE_SSH= BASE=http://127.0.0.1:18080 COORD=http://127.0.0.1:18081 \
    BUDGET_HMC=${BUDGET_HMC:-1.5} RUNS=${RUNS:-32} WORKER_SEC=${WORKER_SEC:-45} \
    ADMIN_FILE=/opt/hackme/.secrets/hackme_admin_token WORKER_TOKEN_FILE=/opt/hackme/.secrets/hackme_coordinator_worker_token \
    bash scripts/ops/run_prod_multi_container_fuzz.sh"
  exit $?
fi

ADMIN="$(tr -d '\r\n' <"$ADMIN_FILE")"
WORKER_TOK="$(tr -d '\r\n' <"$WORKER_FILE")"
WASM="${WASM:-$ROOT/tasks/artifacts/security/rust_script_push_bounds_guard.wasm}"
[[ -f "$WASM" ]] || { echo "[multi-fuzz] missing wasm: $WASM" >&2; exit 1; }

CID="${CID:-prod-multi-$(date +%Y%m%d-%H%M%S)}"
RUNS="${RUNS:-48}"
WORKER_SEC="${WORKER_SEC:-60}"
BUDGET_HMC="${BUDGET_HMC:-2.0}"
LOG_DIR="${LOG_DIR:-$ROOT/logs/prod_multi_fuzz_${CID}}"
WASM_HEX="$(xxd -p "$WASM" | tr -d '\n')"

mkdir -p "$LOG_DIR"
hdr=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json")

echo "[multi-fuzz] wallet @ $BASE"
curl -fsS "${hdr[@]}" "$BASE/api/wallet" | tee "$LOG_DIR/wallet.json" | jq -c '{address,balance_on_chain_hmc}'
MINER="$(jq -r '.address' "$LOG_DIR/wallet.json")"
[[ -n "$MINER" && "$MINER" != null ]] || { echo "[multi-fuzz] no payer address" >&2; exit 1; }

echo "[multi-fuzz] create campaign $CID runs=$RUNS"
create_body="$(jq -nc \
  --arg id "$CID" \
  --arg wasm "$WASM_HEX" \
  --argjson runs "$RUNS" \
  --argjson budget "$BUDGET_HMC" \
  '{
    id: $id,
    campaign_type: "property",
    status: "running",
    title: "Multi-container pool fuzz gate",
    budget_runs: $runs,
    budget_seconds: 600,
    budget_hmc: $budget,
    config: {
      pool_distributed: true,
      check_semantics: "detector",
      wasm_check_hex: $wasm,
      seed_corpus: [133452, 999001],
      mutation_rounds: 0,
      queue_depth: 64
    }
  }')"
curl -fsS -X POST "$BASE/api/fuzz/campaigns" "${hdr[@]}" -d "$create_body" \
  | tee "$LOG_DIR/create.json" | jq -c '{ok,pool_sync,escrow:(.escrow.status // "n/a")}'

echo "[multi-fuzz] pool stats before"
curl -fsS "$COORD/api/fuzz/pool/stats" | tee "$LOG_DIR/stats_before.json" | jq -c '{work_done,workers,queue_depth}'

TREASURY_SEED_FILE="${TREASURY_SEED_FILE:-$ROOT/.secrets/hackme_treasury_ed25519_seed.hex}"
TREASURY_SEED=""
[[ -f "$TREASURY_SEED_FILE" ]] && TREASURY_SEED="$(tr -d '\r\n' <"$TREASURY_SEED_FILE")"
export COORD_URL="$COORD" COORD_TOKEN="$WORKER_TOK"
if [[ -n "$TREASURY_SEED" ]]; then
  export HACKME_MINER_ED25519_SEED_HEX="$TREASURY_SEED"
  unset MINER_ADDRESS
else
  export MINER_ADDRESS="$MINER"
fi
export WORKERFUZZ_HTTP_TIMEOUT_SEC="${WORKERFUZZ_HTTP_TIMEOUT_SEC:-120}"
for w in container-a container-b container-c; do
  echo "[multi-fuzz] worker $w (${WORKER_SEC}s)"
  WORKER_ID="$w" timeout "${WORKER_SEC}s" go run ./cmd/workerfuzz -worker "$w" \
    >"$LOG_DIR/worker_${w}.log" 2>&1 || true
done

echo "[multi-fuzz] pool stats after"
curl -fsS "$COORD/api/fuzz/pool/stats" | tee "$LOG_DIR/stats_after.json" | jq .

echo "[multi-fuzz] campaign status"
curl -fsS "${hdr[@]}" "$BASE/api/fuzz/campaigns/$CID" \
  | tee "$LOG_DIR/campaign.json" | jq -c '{status:.campaign.status,runs_done:.campaign.runs_done,findings:(.findings|length)}'

before="$(jq -r '.work_done // 0' "$LOG_DIR/stats_before.json")"
after="$(jq -r '.work_done // 0' "$LOG_DIR/stats_after.json")"
delta=$((after - before))
echo "[multi-fuzz] work_done delta=$delta"
if [[ "$delta" -lt 8 ]]; then
  echo "[multi-fuzz] WARN: low work_done delta (expected >=8 from 3 workers)" >&2
fi

VER="$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_VERSION" 2>/dev/null || echo dev)"
CLI="${FUZZING_CLI:-$ROOT/dist/hackme-fuzzing-${VER}-linux-amd64}"
if [[ -x "$CLI" ]]; then
  echo "[multi-fuzz] B2B wizard tiers"
  export HACKME_FUZZING_BASE="$BASE"
  for pkg in scan audit deep; do
    out="$("$CLI" wizard --base "$BASE" --wasm "$WASM" --package "$pkg" \
      --title "multi-gate-${pkg}-$(date +%s)" --payer-ref "multi-gate:${pkg}" 2>/dev/null)" || {
      echo "[multi-fuzz] wizard $pkg FAILED"
      continue
    }
    echo "$out" | jq -c "{ok,package,campaign_id,pool_distributed,depth_tier,pool_sync}"
    sleep 6
  done
fi

echo "[multi-fuzz] PASS log_dir=$LOG_DIR"
