#!/usr/bin/env bash
# Keep settlement treasury (payer node wallet) funded for timely worker payouts.
#
#   bash scripts/ops/ensure_settlement_treasury_float.sh
#   MIN_FLOAT_HMC=15 TOPUP_HMC=20 bash scripts/ops/ensure_settlement_treasury_float.sh
#
# VPS hub (auto from dev treasury seed):
#   TREASURY_FUND_SEED_HEX=/opt/hackme/.secrets/hackme_treasury_ed25519_seed.hex \
#   CHAIN_BASE=http://127.0.0.1:18080 bash scripts/ops/ensure_settlement_treasury_float.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

TREASURY_ADDR="${TREASURY_ADDR:-HMC-381c0c5e2cfcc714}"
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
MIN_FLOAT_HMC="${MIN_FLOAT_HMC:-15}"
TOPUP_HMC="${TOPUP_HMC:-20}"
MAX_GENESIS_TOPUP_24H_HMC="${MAX_GENESIS_TOPUP_24H_HMC:-25}"
CATCHUP_TOPUP_HMC="${CATCHUP_TOPUP_HMC:-180}"
CATCHUP_UNPAID_TRIGGER_HMC="${CATCHUP_UNPAID_TRIGGER_HMC:-20}"
SKIP_GENESIS_TOPUP_GUARD="${SKIP_GENESIS_TOPUP_GUARD:-0}"
MIN_SETTLE_HMC="${MIN_SETTLE_HMC:-0.0001}"
DATA_DIR="${HACKME_DATA_DIR:-${DATA_DIR:-$ROOT/logs/desktop/data}}"
TREASURY_FUND_SEED_HEX="${TREASURY_FUND_SEED_HEX:-${ROOT}/.secrets/hackme_treasury_ed25519_seed.hex}"
CURL_MAX_TIME="${CURL_MAX_TIME:-8}"
FUND_TMP_DIR=""

cleanup() {
  [[ -n "$FUND_TMP_DIR" && -d "$FUND_TMP_DIR" ]] && rm -rf "$FUND_TMP_DIR"
}
trap cleanup EXIT

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

fund_data_dir() {
  if [[ -r "$TREASURY_FUND_SEED_HEX" ]]; then
    FUND_TMP_DIR="$(mktemp -d)"
    tr -d '\r\n' <"$TREASURY_FUND_SEED_HEX" >"${FUND_TMP_DIR}/node_ed25519.seed"
    chmod 600 "${FUND_TMP_DIR}/node_ed25519.seed"
    printf '%s' "$FUND_TMP_DIR"
    return 0
  fi
  if [[ -f "${DATA_DIR}/node_ed25519.seed" || -f "${DATA_DIR}/hackme.db" ]]; then
    printf '%s' "$DATA_DIR"
    return 0
  fi
  return 1
}

send_topup() {
  local need="$1"
  local fund_dir
  if ! fund_dir="$(fund_data_dir)"; then
    echo "[treasury-float] WARN no fund seed at ${DATA_DIR} or ${TREASURY_FUND_SEED_HEX}" >&2
    return 1
  fi
  if ! go run ./cmd/sendtransfer \
    -data-dir "$fund_dir" \
    -to "$TREASURY_ADDR" \
    -amount-hmc "$need" \
    -base "$CHAIN_BASE" \
    -memo settlement_treasury_topup; then
    echo "[treasury-float] WARN topup transfer failed" >&2
    return 1
  fi
  local attempt=0
  local target
  target="$(python3 -c "print(float('${need}')*0.85)")"
  while (( attempt < 24 )); do
    local bal
    bal="$(treasury_balance_hmc || echo 0)"
    if python3 -c "import sys; sys.exit(0 if float(sys.argv[1])>=float(sys.argv[2]) else 1)" "$bal" "$target" 2>/dev/null; then
      return 0
    fi
    sleep 5
    attempt=$((attempt + 1))
  done
  echo "[treasury-float] WARN topup sent but balance not confirmed yet" >&2
  return 0
}

bal_hmc="$(treasury_balance_hmc || echo 0)"
if [[ "$bal_hmc" == "0" ]]; then
  echo "[treasury-float] WARN could not read treasury balance from ${CHAIN_BASE} — settlement timer will not block" >&2
fi

fleet_unpaid_hmc="0"
if command -v jq >/dev/null 2>&1; then
  fleet_unpaid_hmc="$(curl -fsS --max-time "$CURL_MAX_TIME" "${CHAIN_BASE}/api/worker/settlement" 2>/dev/null \
    | jq -r '.fleet_unpaid_hmc // .total_unpaid_hmc // 0' 2>/dev/null || echo 0)"
fi

need="$(python3 - "$bal_hmc" "$MIN_FLOAT_HMC" "$TOPUP_HMC" "$fleet_unpaid_hmc" "$CATCHUP_UNPAID_TRIGGER_HMC" "$CATCHUP_TOPUP_HMC" <<'PY'
import sys
bal, mn, top, unpaid, trig, cap = map(float, sys.argv[1:])
need = 0.0
if bal < mn:
    need = max(need, min(top, mn - bal))
if bal < mn and unpaid >= trig:
    # Backlog catch-up: fund settlement for pending fleet payouts (capped).
    need = max(need, min(cap, unpaid + mn - bal))
print(f"{need:.8f}")
PY
)"
if awk -v n="$need" 'BEGIN{exit !(n>0)}'; then
  if [[ "$SKIP_GENESIS_TOPUP_GUARD" != "1" && -f "$ROOT/scripts/ops/treasury_bootstrap_guard.sh" ]]; then
  HACKME_DB="${HACKME_DB:-${ROOT}/data/hackme.db}"
  if [[ ! -f "$HACKME_DB" && -f /opt/hackme/data/hackme.db ]]; then
    HACKME_DB="/opt/hackme/data/hackme.db"
  fi
  if ! HACKME_DB="$HACKME_DB" \
      MAX_GENESIS_TOPUP_24H_HMC="$MAX_GENESIS_TOPUP_24H_HMC" \
      PROPOSED_TOPUP_HMC="$need" \
      SETTLEMENT_BALANCE_HMC="$bal_hmc" \
      MIN_FLOAT_HMC="$MIN_FLOAT_HMC" \
      CHAIN_BASE="$CHAIN_BASE" \
      bash "$ROOT/scripts/ops/treasury_bootstrap_guard.sh"; then
      echo "[treasury-float] SKIP topup — genesis daily budget exceeded" >&2
      need="0"
    fi
  fi
fi
if awk -v n="$need" 'BEGIN{exit !(n>0)}'; then
  echo "[treasury-float] treasury=${TREASURY_ADDR} balance=${bal_hmc} HMC fleet_unpaid=${fleet_unpaid_hmc} — topup ${need} HMC"
  send_topup "$need" || true
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
