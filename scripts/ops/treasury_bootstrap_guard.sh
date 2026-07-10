#!/usr/bin/env bash
# Guard genesis treasury drain: warn when dev → settlement topups exceed daily budget.
#
# Routine autopilot: max MAX_GENESIS_TOPUP_24H_HMC per 24h (default 25).
# Catch-up: when settlement wallet is nearly empty, allow one backlog topup
# (ensure_settlement_treasury_float passes PROPOSED_TOPUP_HMC + SETTLEMENT_BALANCE_HMC).
#
#   bash scripts/ops/treasury_bootstrap_guard.sh
#   HACKME_DB=/opt/hackme/data/hackme.db MAX_GENESIS_TOPUP_24H_HMC=25 bash scripts/ops/treasury_bootstrap_guard.sh
set -euo pipefail

DEV_ADDR="${DEV_TREASURY_ADDR:-HMC-719006d93916ad52}"
SETTLEMENT_ADDR="${TREASURY_ADDR:-HMC-381c0c5e2cfcc714}"
HACKME_DB="${HACKME_DB:-}"
MAX_24H="${MAX_GENESIS_TOPUP_24H_HMC:-25}"
WINDOW_SEC="${GENESIS_TOPUP_WINDOW_SEC:-86400}"
PROPOSED_TOPUP_HMC="${PROPOSED_TOPUP_HMC:-0}"
SETTLEMENT_BALANCE_HMC="${SETTLEMENT_BALANCE_HMC:-}"
MIN_FLOAT_HMC="${MIN_FLOAT_HMC:-15}"
GENESIS_RESERVE_HMC="${GENESIS_RESERVE_HMC:-45000}"
CATCHUP_MAX_SINGLE_HMC="${CATCHUP_MAX_SINGLE_HMC:-180}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if [[ -z "$HACKME_DB" ]]; then
  for candidate in \
    "${HACKME_DATA_DIR:-}/hackme.db" \
    "$ROOT/logs/desktop/data/hackme.db" \
    /opt/hackme/data/hackme.db; do
    if [[ -f "$candidate" ]]; then
      HACKME_DB="$candidate"
      break
    fi
  done
fi

dev_bal="$(curl -fsS --max-time 8 "${CHAIN_BASE:-http://127.0.0.1:18080}/api/address/${DEV_ADDR}" 2>/dev/null \
  | jq -r '.balance_hmc // empty' || true)"

settlement_bal="$SETTLEMENT_BALANCE_HMC"
if [[ -z "$settlement_bal" ]]; then
  settlement_bal="$(curl -fsS --max-time 8 "${CHAIN_BASE:-http://127.0.0.1:18080}/api/address/${SETTLEMENT_ADDR}" 2>/dev/null \
    | jq -r '.balance_hmc // 0' || echo 0)"
fi

echo "[treasury-guard] dev=${DEV_ADDR} balance=${dev_bal:-?} HMC settlement=${settlement_bal:-?} HMC"

# Critical catch-up: settlement empty, pay fleet backlog (bypass routine 24h cap).
if python3 -c "
import sys
sb, prop, mn, cap, reserve, dev = map(float, sys.argv[1:])
if sb >= mn * 0.25:
    sys.exit(1)
if prop <= 0 or prop > cap:
    sys.exit(1)
if dev > 0 and dev - prop < reserve:
    sys.exit(1)
sys.exit(0)
" "${settlement_bal:-0}" "${PROPOSED_TOPUP_HMC:-0}" "$MIN_FLOAT_HMC" "$CATCHUP_MAX_SINGLE_HMC" "$GENESIS_RESERVE_HMC" "${dev_bal:-0}" 2>/dev/null; then
  echo "[treasury-guard] OK catch-up topup ${PROPOSED_TOPUP_HMC} HMC (settlement low, genesis reserve OK)"
  exit 0
fi

if [[ -z "$HACKME_DB" || ! -f "$HACKME_DB" ]]; then
  echo "[treasury-guard] SKIP no hackme.db (set HACKME_DB)" >&2
  exit 0
fi

if ! command -v sqlite3 >/dev/null; then
  echo "[treasury-guard] SKIP sqlite3 missing" >&2
  exit 0
fi

IFS='|' read -r cnt hmc <<<"$(sqlite3 -separator '|' "$HACKME_DB" "
SELECT COUNT(*), COALESCE(ROUND(SUM(amount_units)/100000000.0, 4), 0)
FROM tx_history
WHERE from_address='${DEV_ADDR}'
  AND to_address='${SETTLEMENT_ADDR}'
  AND status='included'
  AND applied_at >= strftime('%s','now') - ${WINDOW_SEC};
")"
cnt="${cnt:-0}"
hmc="${hmc:-0}"

echo "[treasury-guard] genesis→settlement last ${WINDOW_SEC}s: ${cnt} tx, ${hmc} HMC (routine max=${MAX_24H})"

if python3 -c "import sys; sys.exit(0 if float(sys.argv[1]) <= float(sys.argv[2]) else 1)" "${hmc:-0}" "$MAX_24H" 2>/dev/null; then
  echo "[treasury-guard] OK"
  exit 0
fi

echo "[treasury-guard] WARN genesis topup ${hmc} HMC exceeds routine ${MAX_24H} HMC / ${WINDOW_SEC}s — wait for window or low settlement catch-up" >&2
exit 1
