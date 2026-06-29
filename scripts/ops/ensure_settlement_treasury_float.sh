#!/usr/bin/env bash
# Keep settlement treasury (payer node wallet) funded for timely worker payouts.
#
#   bash scripts/ops/ensure_settlement_treasury_float.sh
#   MIN_FLOAT_HMC=50 TOPUP_HMC=30 bash scripts/ops/ensure_settlement_treasury_float.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

TREASURY_ADDR="${TREASURY_ADDR:-HMC-381c0c5e2cfcc714}"
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
MIN_FLOAT_HMC="${MIN_FLOAT_HMC:-40}"
TOPUP_HMC="${TOPUP_HMC:-50}"
MIN_SETTLE_HMC="${MIN_SETTLE_HMC:-0.0001}"
DATA_DIR="${HACKME_DATA_DIR:-${DATA_DIR:-$ROOT/logs/desktop/data}}"
CURL_MAX_TIME="${CURL_MAX_TIME:-8}"

treasury_balance_hmc() {
  local raw=""
  local attempt
  for attempt in 1 2 3; do
    raw="$(curl -fsS --max-time "$CURL_MAX_TIME" "${CHAIN_BASE}/api/address/${TREASURY_ADDR}" 2>/dev/null || true)"
    if [[ -n "$raw" ]]; then
      jq -r '(.balance_units // 0) / 100000000' <<<"$raw" 2>/dev/null && return 0
    fi
    sleep 1
  done
  echo "0"
  return 1
}

bal_hmc="$(treasury_balance_hmc || echo 0)"
if [[ "$bal_hmc" == "0" ]]; then
  echo "[treasury-float] WARN could not read treasury balance from ${CHAIN_BASE} — settlement timer will not block" >&2
fi
need="$(python3 - "$bal_hmc" "$MIN_FLOAT_HMC" "$TOPUP_HMC" <<'PY'
import sys
bal, mn, top = map(float, sys.argv[1:])
if bal >= mn:
    print("0")
else:
    print(f"{max(top, mn - bal):.8f}")
PY
)"
if awk -v n="$need" 'BEGIN{exit !(n>0)}'; then
  echo "[treasury-float] treasury=${TREASURY_ADDR} balance=${bal_hmc} HMC < min=${MIN_FLOAT_HMC} — topup ${need} HMC"
  if [[ -f "${DATA_DIR}/node_ed25519.seed" || -f "${DATA_DIR}/hackme.db" ]]; then
    if ! go run ./cmd/sendtransfer \
      -data-dir "$DATA_DIR" \
      -to "$TREASURY_ADDR" \
      -amount-hmc "$need" \
      -base "$CHAIN_BASE" \
      -memo settlement_treasury_topup; then
      echo "[treasury-float] WARN topup failed (operator seed / canonical); continuing if treasury can settle" >&2
    fi
  else
    echo "[treasury-float] WARN no operator seed at ${DATA_DIR} — skip topup (fund treasury manually or run from desktop)" >&2
  fi
  bal_hmc="$(treasury_balance_hmc || echo "$bal_hmc")"
fi

if awk -v b="$bal_hmc" -v m="$MIN_FLOAT_HMC" 'BEGIN{exit !(b>=m)}'; then
  echo "[treasury-float] OK treasury=${TREASURY_ADDR} balance=${bal_hmc} HMC (min=${MIN_FLOAT_HMC})"
  exit 0
fi
# Allow settlement when treasury can cover at least one min payout (avoid timer deadlock).
if awk -v b="$bal_hmc" -v s="$MIN_SETTLE_HMC" 'BEGIN{exit !(b>=s)}'; then
  echo "[treasury-float] WARN treasury=${bal_hmc} HMC < min=${MIN_FLOAT_HMC} but >= min_settle=${MIN_SETTLE_HMC} — settlement may proceed (chunked)" >&2
  exit 0
fi
echo "[treasury-float] ERROR treasury=${bal_hmc} HMC too low for any payout" >&2
exit 1
