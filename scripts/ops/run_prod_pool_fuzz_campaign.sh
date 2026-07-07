#!/usr/bin/env bash
# Live production pool fuzz campaign (escrow 20/80 + distributed workers).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

BASE="${BASE:-https://hackme.tech}"
COORD="${COORD:-${BASE}/pool/coordinator}"
ADMIN="$(tr -d '\r\n' < "${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}")"
WORKER_TOK="$(tr -d '\r\n' < "${WORKER_TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_worker_token}")"

CID="${CID:-live-pool-fuzz-$(date +%Y%m%d-%H%M%S)}"
BUDGET_HMC="${BUDGET_HMC:-2.0}"
RUNS="${RUNS:-32}"
WORKER_SEC="${WORKER_SEC:-45}"
# Production coordinator can be slow; workerfuzz uses 120s HTTP client timeout when COORD_URL contains hackme.tech
export WORKERFUZZ_HTTP_TIMEOUT_SEC="${WORKERFUZZ_HTTP_TIMEOUT_SEC:-120}"
LOG_DIR="${LOG_DIR:-$ROOT/logs/prod_pool_fuzz_${CID}}"

WASM="${ROOT}/tasks/artifacts/security/rust_script_push_bounds_guard.wasm"
[[ -f "$WASM" ]] || { echo "missing $WASM" >&2; exit 1; }
WASM_HEX="$(xxd -p "$WASM" | tr -d '\n')"

mkdir -p "$LOG_DIR"
hdr_admin=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json")

echo "[prod-pool-fuzz] wallet"
curl -fsS "${hdr_admin[@]}" "$BASE/api/wallet" | tee "$LOG_DIR/wallet.json" | jq -c '{address,balance_hmc}'

MINER="$(jq -r '.address' "$LOG_DIR/wallet.json")"
echo "[prod-pool-fuzz] miner payout address: $MINER"

echo "[prod-pool-fuzz] create campaign $CID budget_hmc=$BUDGET_HMC runs=$RUNS"
create_body="$(jq -nc \
  --arg id "$CID" \
  --arg wasm "$WASM_HEX" \
  --argjson runs "$RUNS" \
  --argjson budget "$BUDGET_HMC" \
  '{
    id: $id,
    campaign_type: "property",
    status: "running",
    title: "Live pool fuzz — Script push bounds guard",
    description: "Production distributed fuzz v2 + escrow 20/80 on hackme.tech",
    budget_runs: $runs,
    budget_seconds: 600,
    budget_hmc: $budget,
    config: {
      pool_distributed: true,
      auto_runner: "0",
      check_semantics: "detector",
      wasm_check_hex: $wasm,
      seed_corpus: [133452, 999001],
      mutation_rounds: 0,
      queue_depth: 64
    }
  }')"

curl -fsS -X POST "$BASE/api/fuzz/campaigns" "${hdr_admin[@]}" -d "$create_body" \
  | tee "$LOG_DIR/create.json" | jq -c '{ok,pool_sync,escrow:.escrow.status,campaign:.campaign.status,report_token:.customer_report_token}'

REPORT_TOKEN="$(jq -r '.customer_report_token // empty' "$LOG_DIR/create.json")"
if [[ -z "$REPORT_TOKEN" ]]; then
  echo "[prod-pool-fuzz] WARN: no report token in create response" >&2
fi

echo "[prod-pool-fuzz] coordinator pool stats (before)"
curl -fsS "$COORD/api/fuzz/pool/stats" | tee "$LOG_DIR/pool_stats_before.json" | jq .

echo "[prod-pool-fuzz] run workerfuzz ${WORKER_SEC}s x2"
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
for w in prod-fuzz-a prod-fuzz-b; do
  WORKER_ID="$w" timeout "${WORKER_SEC}s" go run ./cmd/workerfuzz -worker "$w" -timeout-ms 800 \
    >"$LOG_DIR/worker_${w}.log" 2>&1 || true
done

echo "[prod-pool-fuzz] coordinator pool stats (after)"
curl -fsS "$COORD/api/fuzz/pool/stats" | tee "$LOG_DIR/pool_stats_after.json" | jq .

echo "[prod-pool-fuzz] campaign status"
curl -fsS "${hdr_admin[@]}" "$BASE/api/fuzz/campaigns/$CID" | tee "$LOG_DIR/campaign.json" | jq -c '{id:.campaign.id,status:.campaign.status,summary:.campaign.summary}'

echo "[prod-pool-fuzz] escrow"
curl -fsS "${hdr_admin[@]}" "$BASE/api/fuzz/campaigns/$CID/escrow" 2>/dev/null | tee "$LOG_DIR/escrow.json" | jq . || echo "no escrow row"

report_hdr=(-H "X-Hackme-Admin-Token: $ADMIN")
[[ -n "$REPORT_TOKEN" ]] && report_hdr+=(-H "X-Hackme-Report-Token: $REPORT_TOKEN")

echo "[prod-pool-fuzz] report json"
curl -fsS "${report_hdr[@]}" "$BASE/api/fuzz/campaigns/$CID/report?format=json&limit=50" \
  | tee "$LOG_DIR/report.json" | jq -c '{ok,runs_done:.summary.runs_done,findings:(.findings|length),verdict:.verdict}'

echo "[prod-pool-fuzz] proof-bundle"
curl -fsS "${report_hdr[@]}" "$BASE/api/fuzz/campaigns/$CID/proof-bundle" \
  | tee "$LOG_DIR/proof_bundle.json" | jq -c '{ok,findings:(.findings|length),escrow:.escrow.status}' 2>/dev/null || true

echo "[prod-pool-fuzz] marketplace list snippet"
curl -fsS "$COORD/api/fuzz/pool/campaigns/list" | jq --arg id "$CID" '.campaigns[]? | select(.id==$id)' | tee "$LOG_DIR/marketplace_row.json"

python3 - <<PY >"$LOG_DIR/SUMMARY.md"
import json, os
log = os.environ["LOG_DIR"]
def load(n):
    p = os.path.join(log, n)
    return json.load(open(p)) if os.path.isfile(p) else {}
c = load("create.json")
camp = load("campaign.json").get("campaign", {})
esc = load("escrow.json").get("escrow", {})
rep = load("report.json")
pb = load("proof_bundle.json")
st0 = load("pool_stats_before.json")
st1 = load("pool_stats_after.json")
print("# Production pool fuzz campaign report\n")
print(f"- **Campaign ID:** \`{os.environ['CID']}\`")
print(f"- **Budget:** {os.environ['BUDGET_HMC']} HMC / {os.environ['RUNS']} runs (escrow 20/80)")
print(f"- **Status:** {camp.get('status','?')}")
print(f"- **Runs done (summary):** {camp.get('summary',{}).get('runs_done','?')}")
print(f"- **Escrow:** {esc.get('status','n/a')} — runs paid {esc.get('runs_paid_hmc',0)} HMC, bounty paid {esc.get('bounty_paid_hmc',0)} HMC")
print(f"- **Findings (report):** {len(rep.get('findings') or [])}")
print(f"- **Pool work done:** {st0.get('work_done',0)} → {st1.get('work_done',0)}")
print(f"- **Report URL:** {os.environ['BASE']}/api/fuzz/campaigns/{os.environ['CID']}/report.html")
print(f"- **Marketplace:** {os.environ['BASE']}/fuzz-marketplace.html")
if rep.get('verdict'):
    print(f"- **Verdict:** {rep.get('verdict')}")
print("\n## Artifacts\n")
for f in sorted(os.listdir(log)):
    if f.endswith(('.json','.md','.log')):
        print(f"- \`{f}\`")
PY

echo "[prod-pool-fuzz] SUMMARY: $LOG_DIR/SUMMARY.md"
cat "$LOG_DIR/SUMMARY.md"
