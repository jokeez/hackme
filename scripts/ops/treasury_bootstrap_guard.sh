#!/usr/bin/env bash
# Guard genesis treasury drain: warn when dev → settlement topups exceed daily budget.
#
# Routine autopilot: max MAX_GENESIS_TOPUP_24H_HMC per 24h (default 25).
# Catch-up: settlement below min float OR fleet unpaid backlog — bypass routine 24h cap
# when genesis still above GENESIS_CATCHUP_RESERVE_HMC (default 10k).
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
FLEET_UNPAID_HMC="${FLEET_UNPAID_HMC:-}"
MIN_FLOAT_HMC="${MIN_FLOAT_HMC:-15}"
GENESIS_RESERVE_HMC="${GENESIS_RESERVE_HMC:-30000}"
GENESIS_CATCHUP_RESERVE_HMC="${GENESIS_CATCHUP_RESERVE_HMC:-10000}"
CATCHUP_UNPAID_TRIGGER_HMC="${CATCHUP_UNPAID_TRIGGER_HMC:-20}"
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

if [[ -z "$FLEET_UNPAID_HMC" ]]; then
  FLEET_UNPAID_HMC="$(curl -fsS --max-time 8 "${CHAIN_BASE:-http://127.0.0.1:18080}/api/worker/settlement" 2>/dev/null \
    | jq -r '.fleet_unpaid_hmc // .total_unpaid_hmc // 0' 2>/dev/null || echo 0)"
fi

dev_bal="$(curl -fsS --max-time 8 "${CHAIN_BASE:-http://127.0.0.1:18080}/api/address/${DEV_ADDR}" 2>/dev/null \
  | jq -r '.balance_hmc // empty' || true)"

settlement_bal="$SETTLEMENT_BALANCE_HMC"
if [[ -z "$settlement_bal" ]]; then
  settlement_bal="$(curl -fsS --max-time 8 "${CHAIN_BASE:-http://127.0.0.1:18080}/api/address/${SETTLEMENT_ADDR}" 2>/dev/null \
    | jq -r '.balance_hmc // 0' || echo 0)"
fi

echo "[treasury-guard] dev=${DEV_ADDR} balance=${dev_bal:-?} HMC settlement=${settlement_bal:-?} HMC fleet_unpaid=${FLEET_UNPAID_HMC:-0} HMC"

catchup_reason="$(python3 - "$settlement_bal" "$FLEET_UNPAID_HMC" "$PROPOSED_TOPUP_HMC" "$MIN_FLOAT_HMC" \
  "$CATCHUP_UNPAID_TRIGGER_HMC" "$CATCHUP_MAX_SINGLE_HMC" "${dev_bal:-0}" \
  "$GENESIS_CATCHUP_RESERVE_HMC" <<'PY' || true
import sys
sb, unpaid, prop, mn, trig, cap, dev, reserve = map(float, sys.argv[1:])
if prop <= 0 or prop > cap:
    sys.exit(2)
if dev > 0 and dev - prop < reserve:
    sys.exit(3)
critical = sb < mn * 0.25
backlog = sb < mn and unpaid >= trig
if critical:
    print("settlement_critical")
    sys.exit(0)
if backlog:
    print("fleet_unpaid_backlog")
    sys.exit(0)
sys.exit(1)
PY
)"
catchup_rc=$?

if [[ "$catchup_rc" -eq 0 && -n "$catchup_reason" ]]; then
  echo "[treasury-guard] OK catch-up topup ${PROPOSED_TOPUP_HMC} HMC (${catchup_reason}; genesis reserve floor ${GENESIS_CATCHUP_RESERVE_HMC})"
  exit 0
fi
if [[ "$catchup_rc" -eq 3 ]]; then
  echo "[treasury-guard] BLOCK catch-up ${PROPOSED_TOPUP_HMC} HMC — dev would fall below catchup reserve ${GENESIS_CATCHUP_RESERVE_HMC}" >&2
  exit 1
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
  if python3 -c "import sys; d,p,r=map(float,sys.argv[1:]); sys.exit(0 if d<=0 or d-p>=r else 1)" "${dev_bal:-0}" "${PROPOSED_TOPUP_HMC:-0}" "$GENESIS_RESERVE_HMC" 2>/dev/null; then
    echo "[treasury-guard] OK"
    exit 0
  fi
  echo "[treasury-guard] BLOCK routine topup — dev would fall below reserve ${GENESIS_RESERVE_HMC}" >&2
  exit 1
fi

# Daily cap exceeded — still allow small routine topup if dev healthy and settlement under min.
if awk -v p="${PROPOSED_TOPUP_HMC:-0}" -v mn="$MIN_FLOAT_HMC" -v sb="${settlement_bal:-0}" \
  'BEGIN{exit !(p>0 && sb<mn)}' 2>/dev/null; then
  if python3 -c "import sys; d,p,r=map(float,sys.argv[1:]); sys.exit(0 if d<=0 or d-p>=r else 1)" \
    "${dev_bal:-0}" "${PROPOSED_TOPUP_HMC:-0}" "$GENESIS_CATCHUP_RESERVE_HMC" 2>/dev/null; then
    echo "[treasury-guard] OK routine gap-fill ${PROPOSED_TOPUP_HMC} HMC (24h cap exceeded but settlement < min)" >&2
    exit 0
  fi
fi

echo "[treasury-guard] WARN genesis topup ${hmc} HMC exceeds routine ${MAX_24H} HMC / ${WINDOW_SEC}s — wait for window or backlog catch-up" >&2
exit 1
