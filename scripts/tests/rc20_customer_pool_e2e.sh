#!/usr/bin/env bash
# Live rc20 E2E: treasury → customer → Fund DevFee → hub confirm → Dig → wait runs → cancel.
# Secrets never printed. Verdict: $OUT/VERDICT.json
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-/tmp/hackme-rc20-e2e-$STAMP}"
mkdir -p "$OUT" && chmod 700 "$OUT"
HUB="${HACKME_ORDERS_HUB_URL:-https://hackme.tech}"
BASE="${HACKME_CHAIN_BASE:-https://hackme.tech}"
PUB_FILE="${HACKME_ORDERS_PUBLISH_TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_orders_publish_token}"
TREASURY_SEED="${TREASURY_FUND_SEED_HEX:-$ROOT/.secrets/hackme_treasury_ed25519_seed.hex}"
TREASURY=HMC-719006d93916ad52
DEVFEE=HMC-719006d93916ad52
FUND_HMC="${FUND_HMC:-15}"
BUDGET_HMC="${BUDGET_HMC:-1}"
WAIT_RUNS_SEC="${WAIT_RUNS_SEC:-240}"

log() { echo "[rc20-e2e $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/run.log"; }
die() { log "FAIL: $*"; printf '{"ok":false,"error":%s}\n' "$(printf '%s' "$*" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')" >"$OUT/VERDICT.json"; exit 1; }

[[ -r "$PUB_FILE" ]] || die "publish token file missing"
[[ -r "$TREASURY_SEED" ]] || die "treasury seed missing"

go build -o "$OUT/show_node_addr" ./tools/show_node_addr
go build -o "$OUT/sendtransfer" ./cmd/sendtransfer
go build -o "$OUT/rc20_pool_client" ./tools/rc20_pool_client

CUST="$OUT/customer"
mkdir -p "$CUST" && chmod 700 "$CUST"
python3 - <<PY
import os, secrets
p="$CUST/node_ed25519.seed"
# nodecrypto expects hex seed with no trailing newline garbage
open(p,"w").write(secrets.token_hex(32))
os.chmod(p, 0o600)
PY
OWNER="$("$OUT/show_node_addr" "$CUST")"
log "customer=$OWNER"
echo "$OWNER" >"$OUT/customer_addr.txt"

TMP_T=$(mktemp -d) && chmod 700 "$TMP_T"
cleanup() { rm -rf "$TMP_T"; }
trap cleanup EXIT
tr -d '\r\n' <"$TREASURY_SEED" >"$TMP_T/node_ed25519.seed"
chmod 600 "$TMP_T/node_ed25519.seed"
FROM="$("$OUT/show_node_addr" "$TMP_T")"
[[ "$FROM" == "$TREASURY" ]] || die "treasury addr mismatch"

# 1) Treasury tops up customer on-chain (amount + fee headroom)
TOPUP=$(python3 -c "print($FUND_HMC + 0.01)")
log "treasury topup customer $TOPUP HMC"
HACKME_DATA_DIR="$TMP_T" "$OUT/sendtransfer" -base "$BASE" -to "$OWNER" -amount-hmc "$TOPUP" -memo "rc20_e2e_topup" \
  >"$OUT/topup.json" 2>"$OUT/topup.err" || die "topup failed"
TOP_TX=$(python3 -c 'import json; print(json.load(open("'"$OUT/topup.json"'")).get("tx_hash",""))')
[[ -n "$TOP_TX" ]] || die "topup missing tx_hash"
for i in $(seq 1 40); do
  st=$(curl -fsS --max-time 8 "$BASE/api/tx/$TOP_TX" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))' 2>/dev/null || true)
  log "topup status=$st"
  [[ "$st" == "included" || "$st" == "applied" || "$st" == "confirmed" ]] && break
  sleep 2
done

# 2) Customer Fund Orders → DevFee
MEMO="orders_fund_v1:${OWNER}"
log "customer fund DevFee $FUND_HMC memo=$MEMO"
HACKME_DATA_DIR="$CUST" "$OUT/sendtransfer" -base "$BASE" -to "$DEVFEE" -amount-hmc "$FUND_HMC" -memo "$MEMO" \
  >"$OUT/fund.json" 2>"$OUT/fund.err" || die "fund failed"
TX=$(python3 -c 'import json; print(json.load(open("'"$OUT/fund.json"'")).get("tx_hash",""))')
[[ -n "$TX" ]] || die "fund missing tx_hash"
echo "$TX" >"$OUT/fund_tx_hash.txt"
for i in $(seq 1 40); do
  st=$(curl -fsS --max-time 8 "$BASE/api/tx/$TX" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))' 2>/dev/null || true)
  log "fund status=$st"
  [[ "$st" == "included" || "$st" == "applied" || "$st" == "confirmed" ]] && break
  sleep 2
done

export HACKME_ORDERS_HUB_URL="$HUB"
export HACKME_POOL_ORDERS_PUBLISH_TOKEN
HACKME_POOL_ORDERS_PUBLISH_TOKEN="$(tr -d '\r\n' <"$PUB_FILE")"
export HACKME_DATA_DIR="$CUST"

log "hub fund confirm"
"$OUT/rc20_pool_client" fund-confirm --tx "$TX" --amount-hmc "$FUND_HMC" >"$OUT/fund_confirm.json" 2>"$OUT/fund_confirm.err" \
  || die "fund confirm failed ($(tail -c 300 "$OUT/fund_confirm.err" 2>/dev/null || true) $(tail -c 300 "$OUT/fund_confirm.json" 2>/dev/null || true))"
PRE_BEFORE=$(python3 -c 'import json; j=json.load(open("'"$OUT/fund_confirm.json"'")); print((j.get("funded") or {}).get("spendable_after_hmc") or j.get("spendable_hmc") or "")')
log "prepaid_after_fund=$PRE_BEFORE"

log "hub dig budget=$BUDGET_HMC"
"$OUT/rc20_pool_client" dig --budget-hmc "$BUDGET_HMC" --title "rc20-e2e-$STAMP" >"$OUT/dig.json" 2>"$OUT/dig.err" \
  || die "dig failed ($(tail -c 500 "$OUT/dig.err" 2>/dev/null || true) $(tail -c 500 "$OUT/dig.json" 2>/dev/null || true))"
CID=$(python3 -c 'import json; j=json.load(open("'"$OUT/dig.json"'")); print(j.get("campaign_id") or (j.get("campaign") or {}).get("id") or "")')
[[ -n "$CID" ]] || die "no campaign_id"
echo "$CID" >"$OUT/campaign_id.txt"
log "campaign_id=$CID hub_pool=$(python3 -c 'import json; print(json.load(open("'"$OUT/dig.json"'")).get("hub_pool"))')"

RUNS=0
DEADLINE=$((SECONDS + WAIT_RUNS_SEC))
while (( SECONDS < DEADLINE )); do
  PROG=$(curl -fsS --max-time 12 "${HUB}/pool/coordinator/api/fuzz/pool/campaigns/progress?id=${CID}" 2>/dev/null || true)
  if [[ -n "$PROG" ]]; then
    printf '%s' "$PROG" >"$OUT/progress.json"
    RUNS=$(python3 - <<'PY' "$OUT/progress.json" 2>/dev/null || echo 0
import json,sys
j=json.load(open(sys.argv[1]))
def pick(o):
  if not isinstance(o,dict): return 0
  if "runs_done" in o:
    try: return int(o["runs_done"] or 0)
    except Exception: return 0
  return 0
n=pick(j)
if not n: n=pick(j.get("campaign") or {})
if not n: n=pick(j.get("progress") or {})
arr=j.get("campaigns") or j.get("items") or []
if not n and isinstance(arr,list) and arr: n=pick(arr[0] if isinstance(arr[0],dict) else {})
print(n)
PY
)
  fi
  log "runs_done=$RUNS"
  (( RUNS >= 1 )) && break
  sleep 5
done

log "hub cancel"
"$OUT/rc20_pool_client" cancel --campaign-id "$CID" >"$OUT/cancel.json" 2>"$OUT/cancel.err" \
  || die "cancel failed ($(tail -c 400 "$OUT/cancel.err" 2>/dev/null || true) $(tail -c 400 "$OUT/cancel.json" 2>/dev/null || true))"
PRE_AFTER=$(python3 -c 'import json; print(json.load(open("'"$OUT/cancel.json"'")).get("spendable_hmc",""))')
ESC_STATUS=$(python3 -c 'import json; print((json.load(open("'"$OUT/cancel.json"'")).get("escrow") or {}).get("status",""))')
log "prepaid_after_cancel=$PRE_AFTER escrow=$ESC_STATUS"

python3 - <<PY >"$OUT/VERDICT.json"
import json
v={
  "ok": True,
  "stamp": "$STAMP",
  "owner": "$OWNER",
  "fund_tx": "$TX",
  "campaign_id": "$CID",
  "runs_done": int("$RUNS" or 0),
  "prepaid_after_fund": "$PRE_BEFORE",
  "prepaid_after_cancel": "$PRE_AFTER",
  "escrow_status": "$ESC_STATUS",
  "pickup": int("$RUNS" or 0) >= 1,
  "path": "treasury->customer->DevFee fund->hub confirm->pool dig->cancel",
}
# money path must close escrow / restore prepaid roughly FUND-fee
try:
  before=float("$PRE_BEFORE" or 0)
  after=float("$PRE_AFTER" or 0)
  budget=float("$BUDGET_HMC")
  v["refund_ok"]= after + 1e-6 >= (before - budget) and "$ESC_STATUS"=="closed"
except Exception as e:
  v["refund_ok"]=False
  v["refund_err"]=str(e)
print(json.dumps(v, indent=2))
PY
cat "$OUT/VERDICT.json"
python3 - <<PY
import json,sys
v=json.load(open("$OUT/VERDICT.json"))
if not v.get("refund_ok"):
  print("FAIL: refund/escrow not closed", flush=True); sys.exit(2)
if not v.get("pickup"):
  print("WARN: no miner pickup in window — money path OK", flush=True)
print("PASS money path", flush=True)
PY
