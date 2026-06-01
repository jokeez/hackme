#!/usr/bin/env bash
# Exchange integration readiness probe (HTTP contract from docs/EXCHANGE_LISTING_WALLET_PREP.md).
#
#   BASE=https://hackme.tech bash scripts/ops/exchange_listing_smoke.sh
#   BASE=https://hackme.tech ADMIN_TOKEN=... REAL_SEND=1 AMOUNT_HMC=0.00001 bash scripts/ops/exchange_listing_smoke.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE="${BASE:-https://hackme.tech}"
BASE="${BASE%/}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
REAL_SEND="${REAL_SEND:-0}"
AMOUNT_HMC="${AMOUNT_HMC:-0.00001}"
DEV_FEE="HMC-719006d93916ad52"
PROBE_ADDR="${PROBE_ADDR:-HMC-91fe007e4036c602}"
CURL_MAX="${CURL_MAX:-60}"

pass=0
fail=0
ok() { pass=$((pass + 1)); echo "[exchange-smoke] OK $*"; }
bad() { fail=$((fail + 1)); echo "[exchange-smoke] FAIL $*" >&2; }

require_jq() { command -v jq >/dev/null 2>&1 || { bad "jq required"; exit 1; }; }
require_jq

fetch_status() {
  local url="$1" out=""
  out="$(curl -fsS --max-time "$CURL_MAX" "$url" 2>/dev/null || true)"
  if [[ -n "$out" ]] && echo "$out" | jq -e . >/dev/null 2>&1; then
    printf '%s' "$out"
    return 0
  fi
  return 1
}

st="$(fetch_status "$BASE/api/status" || fetch_status "$BASE/api/status?lite=1" || true)"
if [[ -z "$st" ]]; then
  bad "GET /api/status timeout"
  echo "[exchange-smoke] pass=$pass fail=$fail"
  exit 1
fi
echo "$st" | jq -e '.has_genesis == true' >/dev/null || { bad "has_genesis"; exit 1; }
ok "GET /api/status has_genesis"

chain_id="$(echo "$st" | jq -r '.chain_id // ""')"
policy="$(echo "$st" | jq -r '.economics.policy_hash // ""')"
dev="$(echo "$st" | jq -r '.economics.dev_fee_address // ""')"
mint="$(echo "$st" | jq -r '.economics.total_minted_hmc // 0')"
burn="$(echo "$st" | jq -r '.economics.total_burned_hmc // 0')"
circ="$(echo "$st" | jq -r '.economics.circulating_hmc // 0')"
if [[ -n "$chain_id" ]]; then ok "chain_id=$chain_id"; else bad "empty chain_id"; fi
if [[ "$dev" == "$DEV_FEE" ]]; then ok "dev_fee_address consensus"; else bad "dev_fee_address=$dev want $DEV_FEE"; fi
if [[ -n "$policy" && ${#policy} -ge 32 ]]; then ok "policy_hash present"; else bad "policy_hash missing"; fi
python3 - "$mint" "$burn" "$circ" <<'PY' || bad "economics math"
import sys
m,b,c=map(float,sys.argv[1:4])
assert abs((m-b)-c) < 1e-4, (m,b,c)
PY
ok "economics circulating identity"

addr_json="$(curl -fsS --max-time "$CURL_MAX" "$BASE/api/address/${PROBE_ADDR}")"
bal="$(echo "$addr_json" | jq -r '.balance_units // empty')"
nonce="$(echo "$addr_json" | jq -r '.next_nonce // empty')"
if [[ "$bal" =~ ^[0-9]+$ ]]; then ok "GET /api/address balance_units=$bal"; else bad "address lookup"; fi
if [[ "$nonce" =~ ^[0-9]+$ ]]; then ok "GET /api/address next_nonce=$nonce"; else bad "nonce missing"; fi

code_invalid="$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' -X POST "$BASE/api/tx/send" \
  -H 'Content-Type: application/json' -d '{"tx_type":"transfer_v1","from":"HMC-0000000000000001","to":"'"$PROBE_ADDR"'","amount_units":1,"fee_units":1000,"nonce":0,"timestamp_unix":1,"pubkey_ed25519":"00","sig_ed25519":"00"}' || echo 000)"
if [[ "$code_invalid" == "400" || "$code_invalid" == "401" ]]; then ok "POST /api/tx/send rejects bad sig HTTP $code_invalid"; else bad "bad sig HTTP $code_invalid"; fi

code_lowfee="$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' -X POST "$BASE/api/tx/send" \
  -H 'Content-Type: application/json' -d '{"tx_type":"transfer_v1","from":"'"$PROBE_ADDR"'","to":"'"$DEV_FEE"'","amount_units":1,"fee_units":1,"nonce":0,"timestamp_unix":1,"pubkey_ed25519":"00","sig_ed25519":"00"}' || echo 000)"
if [[ "$code_lowfee" == "400" ]]; then ok "POST /api/tx/send rejects low fee"; else bad "low fee HTTP $code_lowfee"; fi

expl_code="$(curl -sS --max-time 30 -o /dev/null -w '%{http_code}' "$BASE/explorer" 2>/dev/null || curl -sS --max-time 30 -o /dev/null -w '%{http_code}' "$BASE/pool/explorer" 2>/dev/null || echo 000)"
if [[ "$expl_code" == "200" || "$expl_code" == "301" || "$expl_code" == "302" ]]; then ok "explorer HTTP $expl_code"; else bad "explorer HTTP $expl_code"; fi

if [[ "$REAL_SEND" == "1" ]]; then
  [[ -n "$ADMIN_TOKEN" ]] || { bad "REAL_SEND=1 requires ADMIN_TOKEN"; exit 1; }
  SEED="${SEED_FILE:-$ROOT/data/node_ed25519.seed}"
  [[ -f "$SEED" ]] || { bad "seed missing $SEED"; exit 1; }
  echo "[exchange-smoke] REAL_SEND micro transfer $AMOUNT_HMC HMC -> $DEV_FEE"
  tx_out="$(mktemp)"
  if go run "$ROOT/cmd/sendtransfer" -base "$BASE" -to "$DEV_FEE" -amount "$AMOUNT_HMC" -admin-token "$ADMIN_TOKEN" -seed "$SEED" >"$tx_out" 2>&1; then
    ok "real transfer submitted ($(head -1 "$tx_out"))"
  else
    bad "real transfer failed: $(cat "$tx_out")"
  fi
  rm -f "$tx_out"
fi

echo "[exchange-smoke] pass=$pass fail=$fail"
exit $(( fail > 0 ? 1 : 0 ))
