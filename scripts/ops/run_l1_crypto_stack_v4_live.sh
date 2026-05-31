#!/usr/bin/env bash
# L1 Crypto Stack v4 — offline corpus (v3) + live production fuzz on hackme.tech + HTML report.
#
#   bash scripts/ops/run_l1_crypto_stack_v4_live.sh
#   PHASE=live BASE=https://hackme.tech bash scripts/ops/run_l1_crypto_stack_v4_live.sh
#   DEPLOY=1 NODE_SSH=hackme-vps bash scripts/ops/run_l1_crypto_stack_v4_live.sh
#
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

PHASE="${PHASE:-all}"
BASE="${BASE:-https://hackme.tech}"
COORD="${COORD:-${BASE}/pool/coordinator}"
ADMIN_FILE="${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}"
WORKER_TOKEN_FILE="${WORKER_TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_worker_token}"
TREASURY_SEED_FILE="${TREASURY_SEED_FILE:-$ROOT/.secrets/hackme_treasury_ed25519_seed.hex}"

# Min on-chain escrow per campaign is fuzzescrow.MinCampaignBudgetHMC (0.5).
MODULE_BUDGET_HMC="${MODULE_BUDGET_HMC:-0.5}"
if awk -v b="$MODULE_BUDGET_HMC" 'BEGIN{exit !(b+0 < 0.5)}'; then
  echo "[l1-v4] MODULE_BUDGET_HMC=$MODULE_BUDGET_HMC below 0.5 — using 0.5" >&2
  MODULE_BUDGET_HMC=0.5
fi
MODULE_RUNS="${MODULE_RUNS:-24}"
WORKER_SEC="${WORKER_SEC:-50}"
OUT="$ROOT/reports/l1-crypto-stack-v4/live"
mkdir -p "$OUT/campaigns"

require_cmd curl
require_cmd jq
require_cmd python3
require_cmd go
require_cmd xxd

run_offline() {
  echo "[l1-v4] === phase offline (v3 corpus + fidelity) ==="
  bash "$ROOT/scripts/build_upstream_l1_pack.sh" >/dev/null
  bash "$ROOT/scripts/ops/fetch_upstream_pins.sh"
  bash "$ROOT/scripts/ops/verify_upstream_fidelity.sh"
  go run ./tools/hackme_order_gate_golden
  go run ./tools/l1stack_v3_report
}

run_live() {
  local TS
  TS="$(date -u +%Y%m%dT%H%M%SZ)-$$"
  echo "[l1-v4] === phase live (VPS node + public pool) run=$TS ==="
  NODE_SSH="${NODE_SSH:-hackme-vps}"
  NODE_INTERNAL="${NODE_INTERNAL:-http://127.0.0.1:18080}"
  WORKER_TOK="$(head -n1 "$WORKER_TOKEN_FILE" | tr -d '\r\n')"
  [[ -n "$WORKER_TOK" ]] || { echo "[l1-v4] need coordinator worker token" >&2; exit 1; }
  export WORKERFUZZ_HTTP_TIMEOUT_SEC="${WORKERFUZZ_HTTP_TIMEOUT_SEC:-120}"

  echo "[l1-v4] rsync upstream WASM to VPS"
  ssh -o BatchMode=yes "$NODE_SSH" "mkdir -p /opt/hackme/tasks/artifacts/security"
  scp -q "$ROOT"/tasks/artifacts/security/upstream_*.wasm \
    "${NODE_SSH}:/opt/hackme/tasks/artifacts/security/"

  echo "[l1-v4] snapshot before (internal node via SSH)"
  ssh -o BatchMode=yes "$NODE_SSH" "bash -s" -- "$NODE_INTERNAL" "$OUT/wallet_before.json" "$OUT/chain_before.json" <<'REMOTE' | tee "$OUT/internal_before.log"
set -euo pipefail
NODE="$1"
ADMIN=$(tr -d '\r\n' < /opt/hackme/.secrets/hackme_admin_token)
HDR=(-H "X-Hackme-Admin-Token: $ADMIN")
curl -fsS "${HDR[@]}" "$NODE/api/wallet" | tee "/tmp/l1v4_wallet_before.json" | jq -c '{address,balance_hmc}'
curl -fsS "${HDR[@]}" "$NODE/api/status" | tee "/tmp/l1v4_chain_before.json" | jq -c '{tip_height,canonical_tip_height,version,has_genesis}'
REMOTE
  scp -q "${NODE_SSH}:/tmp/l1v4_wallet_before.json" "$OUT/wallet_before.json" 2>/dev/null || true
  scp -q "${NODE_SSH}:/tmp/l1v4_chain_before.json" "$OUT/chain_before.json" 2>/dev/null || true

  curl -fsS --max-time 90 "$COORD/api/pool/stats" | tee "$OUT/pool_before.json" | jq -c '{hashrate,miners,workers}' || echo '{}' >"$OUT/pool_before.json"
  curl -fsS --max-time 90 "$COORD/api/fuzz/pool/stats" | tee "$OUT/fuzz_pool_before.json" | jq . || echo '{}' >"$OUT/fuzz_pool_before.json"

  MINER="$(jq -r '.address' "$OUT/wallet_before.json")"
  TREASURY_SEED=""
  [[ -f "$TREASURY_SEED_FILE" ]] && TREASURY_SEED="$(tr -d '\r\n' <"$TREASURY_SEED_FILE")"
  export COORD_URL="$COORD" COORD_TOKEN="$WORKER_TOK"
  if [[ -n "$TREASURY_SEED" ]]; then
    export HACKME_MINER_ED25519_SEED_HEX="$TREASURY_SEED"
    unset MINER_ADDRESS
  else
    export MINER_ADDRESS="$MINER"
  fi

  MODULES=(
    "Bitcoin|upstream_bitcoin_getscriptop.wasm"
    "Ethereum|upstream_ethereum_value_overflow.wasm"
    "Dogecoin|upstream_dogecoin_hasvalidops.wasm"
    "Litecoin|upstream_litecoin_getscriptop.wasm"
    "HackMe|upstream_hackme_order_gate.wasm"
  )

  : >"$OUT/campaigns.jsonl"
  local i=0
  while IFS='|' read -r chain wasm_file; do
    i=$((i + 1))
    local guard="${wasm_file%.wasm}"
    local wasm="$ROOT/tasks/artifacts/security/$wasm_file"
    [[ -f "$wasm" ]] || { echo "[l1-v4] missing $wasm" >&2; exit 1; }
    local cid logdir
    cid="l1v4-${chain,,}-${TS}-m${i}"
    logdir="$OUT/campaigns/$cid"
    mkdir -p "$logdir"

    echo ""
    echo "[l1-v4] campaign $i/$chain budget=${MODULE_BUDGET_HMC} runs=${MODULE_RUNS} id=$cid"

    ssh -o BatchMode=yes "$NODE_SSH" "bash -s" -- \
      "$NODE_INTERNAL" "$cid" "$chain" "$MODULE_BUDGET_HMC" "$MODULE_RUNS" "$wasm_file" <<'REMOTE' \
      | tee "$logdir/create.json" | jq -c '{ok,escrow:.escrow.status,campaign:.campaign.status}'
set -euo pipefail
NODE="$1"; CID="$2"; CHAIN="$3"; BUDGET="$4"; RUNS="$5"; WASM_FILE="$6"
TITLE="L1 v4 upstream ${CHAIN} guard"
ADMIN=$(tr -d '\r\n' < /opt/hackme/.secrets/hackme_admin_token)
WASM_PATH="/opt/hackme/tasks/artifacts/security/$WASM_FILE"
WASM_HEX=$(xxd -p "$WASM_PATH" | tr -d '\n')
BODY=$(jq -nc \
  --arg id "$CID" --arg chain "$CHAIN" --arg title "$TITLE" --arg wasm "$WASM_HEX" \
  --argjson runs "$RUNS" --argjson budget "$BUDGET" \
  '{
    id: $id, campaign_type: "property", status: "running",
    title: $title,
    description: ("Production useful-PoW fuzz on upstream " + $chain + " WASM guard (L1 v4)"),
    budget_runs: $runs, budget_seconds: 900, budget_hmc: $budget,
    owner_ref: "research:l1-crypto-stack-v4",
    config: {
      pool_distributed: true, auto_runner: "0", check_semantics: "detector",
      wasm_check_hex: $wasm, seed_corpus: [133452, 999001, 420042],
      mutation_rounds: 1, queue_depth: 64
    }
  }')
curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json" \
  "$NODE/api/fuzz/campaigns" -d "$BODY"
REMOTE

    local report_token
    report_token="$(jq -r '.customer_report_token // empty' "$logdir/create.json")"

    echo "[l1-v4] workerfuzz ${WORKER_SEC}s × 3 for $cid"
    for w in "l1v4-${chain,,}-a" "l1v4-${chain,,}-b" "l1v4-${chain,,}-c"; do
      WORKER_ID="$w" timeout "${WORKER_SEC}s" go run ./cmd/workerfuzz -worker "$w" -timeout-ms 800 \
        >"$logdir/worker_${w}.log" 2>&1 || true
    done

    sleep 8
    ssh -o BatchMode=yes "$NODE_SSH" "bash -s" -- "$NODE_INTERNAL" "$cid" "$report_token" <<'REMOTE' \
      >"$logdir/remote_fetch.json"
set -euo pipefail
NODE="$1"; CID="$2"; TOKEN="$3"
ADMIN=$(tr -d '\r\n' < /opt/hackme/.secrets/hackme_admin_token)
HDR=(-H "X-Hackme-Admin-Token: $ADMIN")
[[ -n "$TOKEN" ]] && HDR+=(-H "X-Hackme-Report-Token: $TOKEN")
curl -fsS "${HDR[@]}" "$NODE/api/fuzz/campaigns/$CID" > "/tmp/${CID}_camp.json"
curl -fsS "${HDR[@]}" "$NODE/api/fuzz/campaigns/$CID/escrow" > "/tmp/${CID}_escrow.json" 2>/dev/null || echo '{}' > "/tmp/${CID}_escrow.json"
curl -fsS "${HDR[@]}" "$NODE/api/fuzz/campaigns/$CID/report?format=json&limit=40" > "/tmp/${CID}_report.json"
curl -fsS "${HDR[@]}" "$NODE/api/fuzz/campaigns/$CID/report?format=html" > "/tmp/${CID}_report.html" 2>/dev/null || true
jq -nc \
  --slurpfile camp "/tmp/${CID}_camp.json" \
  --slurpfile esc "/tmp/${CID}_escrow.json" \
  --slurpfile rep "/tmp/${CID}_report.json" \
  '{campaign:$camp[0],escrow:$esc[0],report:$rep[0]}'
REMOTE
    jq '.campaign' "$logdir/remote_fetch.json" >"$logdir/campaign.json"
    jq '.escrow' "$logdir/remote_fetch.json" >"$logdir/escrow.json"
    jq '.report' "$logdir/remote_fetch.json" >"$logdir/report.json"
    scp -q "${NODE_SSH}:/tmp/${cid}_report.html" "$logdir/report.html" 2>/dev/null || true

    jq -nc \
      --arg chain "$chain" --arg guard "$guard" --arg cid "$cid" \
      --argjson budget "$MODULE_BUDGET_HMC" --argjson runs "$MODULE_RUNS" \
      --slurpfile camp "$logdir/campaign.json" \
      --slurpfile esc "$logdir/escrow.json" \
      --slurpfile rep "$logdir/report.json" \
      '{
        chain: $chain, guard: $guard, campaign_id: $cid,
        budget_hmc: $budget, budget_runs: $runs,
        status: ($camp[0].campaign.status // "?"),
        runs_done: ($camp[0].campaign.summary.runs_done // $rep[0].summary.runs_done // $rep[0].runs_done // 0),
        verdict: ($rep[0].verdict // $rep[0].security_summary.verdict // "?"),
        findings: (($rep[0].findings // []) | length),
        escrow_status: ($esc[0].escrow.status // "n/a"),
        runs_paid_hmc: ($esc[0].escrow.runs_paid_hmc // 0),
        bounty_paid_hmc: ($esc[0].escrow.bounty_paid_hmc // 0),
        locked_hmc: ($esc[0].escrow.locked_hmc // $budget),
        report_url: ("https://hackme.tech/api/fuzz/campaigns/" + $cid + "/report.html")
      }' >>"$OUT/campaigns.jsonl"
  done < <(printf '%s\n' "${MODULES[@]}")

  ssh -o BatchMode=yes "$NODE_SSH" "bash -s" -- "$NODE_INTERNAL" <<'REMOTE'
set -euo pipefail
NODE="$1"
ADMIN=$(tr -d '\r\n' < /opt/hackme/.secrets/hackme_admin_token)
HDR=(-H "X-Hackme-Admin-Token: $ADMIN")
curl -fsS "${HDR[@]}" "$NODE/api/wallet" > /tmp/l1v4_wallet_after.json
curl -fsS "${HDR[@]}" "$NODE/api/status" > /tmp/l1v4_chain_after.json
REMOTE
  scp -q "${NODE_SSH}:/tmp/l1v4_wallet_after.json" "$OUT/wallet_after.json"
  scp -q "${NODE_SSH}:/tmp/l1v4_chain_after.json" "$OUT/chain_after.json"
  jq -c '{address,balance_hmc}' "$OUT/wallet_after.json"
  jq -c '{tip_height,canonical_tip_height}' "$OUT/chain_after.json"

  curl -fsS --max-time 90 "$COORD/api/pool/stats" | tee "$OUT/pool_after.json" | jq -c '{hashrate,miners,workers}' || true
  curl -fsS --max-time 90 "$COORD/api/fuzz/pool/stats" | tee "$OUT/fuzz_pool_after.json" | jq .

  python3 - <<PY
import json, pathlib
out = pathlib.Path("$OUT")
campaigns = [json.loads(l) for l in out.joinpath("campaigns.jsonl").read_text().splitlines() if l.strip()]
wb = json.loads(out.joinpath("wallet_before.json").read_text())
wa = json.loads(out.joinpath("wallet_after.json").read_text())
cb = json.loads(out.joinpath("chain_before.json").read_text())
ca = json.loads(out.joinpath("chain_after.json").read_text())
spent = round(sum(float(c.get("locked_hmc",0) or 0) for c in campaigns), 8)
if spent == 0:
  spent = round(max(0.0, float(wb.get("balance_hmc",0)) - float(wa.get("balance_hmc",0))), 8)
summary = {
  "timestamp": "$TS",
  "base": "$BASE",
  "node_internal": "$NODE_INTERNAL",
  "phase": "live",
  "wallet_before": wb,
  "wallet_after": wa,
  "wallet_spent_hmc": round(spent, 8),
  "chain_before": cb,
  "chain_after": ca,
  "blocks_mined_delta": int(ca.get("tip_height",0) or 0) - int(cb.get("tip_height",0) or 0),
  "campaigns": campaigns,
  "total_campaigns": len(campaigns),
  "total_runs_done": sum(c.get("runs_done",0) for c in campaigns),
  "total_runs_paid_hmc": round(sum(float(c.get("runs_paid_hmc",0) or 0) for c in campaigns), 8),
  "total_bounty_paid_hmc": round(sum(float(c.get("bounty_paid_hmc",0) or 0) for c in campaigns), 8),
}
out.joinpath("summary.json").write_text(json.dumps(summary, indent=2))
print(json.dumps({k: summary[k] for k in ("wallet_spent_hmc","total_campaigns","total_runs_done","blocks_mined_delta")}, indent=2))
PY
}

run_report() {
  echo "[l1-v4] === phase report (HTML) ==="
  go run ./tools/l1stack_v4_report
  bash "$ROOT/scripts/ops/l1_crypto_stack_v4_gate.sh"
}

run_deploy() {
  if [[ "${DEPLOY:-0}" != "1" ]]; then
    echo "[l1-v4] skip deploy (set DEPLOY=1 NODE_SSH=...)"
    return 0
  fi
  echo "[l1-v4] === phase deploy site ==="
  NODE_SSH="${NODE_SSH:-hackme-vps}" NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}" SKIP_DIST=1 \
    bash "$ROOT/scripts/ops/deploy_hackme_site.sh"
}

case "$PHASE" in
  offline) run_offline ;;
  live) run_live ;;
  report) run_report ;;
  deploy) run_deploy ;;
  all)
    run_offline
    run_live
    run_report
    run_deploy
    ;;
  *)
    echo "PHASE must be offline|live|report|deploy|all" >&2
    exit 1
    ;;
esac

echo "[l1-v4] done"
echo "  summary: $OUT/summary.json"
echo "  html:    file://$ROOT/web/site/reports/l1-crypto-stack-v4.html"
echo "  live:    https://hackme.tech/reports/l1-crypto-stack-v4.html"
