#!/usr/bin/env bash
# Pool subsidy budget: chain emission vs coordinator accrual vs genesis topups.
#
# Maintains a small state file to estimate accrual HMC/hour between runs.
#
#   bash scripts/ops/pool_subsidy_budget_snapshot.sh
#   NODE_SSH=hackme-vps bash scripts/ops/pool_subsidy_budget_snapshot.sh
#
# Exit 0 always (monitoring); exit 2 when SUBSIDY_WARN=1 and subsidy_ratio > threshold.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"

if [[ -n "$NODE_SSH" ]]; then
  rsync -az "$ROOT/scripts/ops/pool_subsidy_budget_snapshot.sh" \
    "$NODE_SSH:$DEPLOY/scripts/ops/" 2>/dev/null || true
  ssh -o BatchMode=yes "$NODE_SSH" "DEPLOY='$DEPLOY' bash '$DEPLOY/scripts/ops/pool_subsidy_budget_snapshot.sh'"
  exit $?
fi

CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
HACKME_DB="${HACKME_DB:-${DEPLOY}/data/hackme.db}"
STATE_FILE="${POOL_SUBSIDY_STATE_FILE:-${DEPLOY}/data/pool_subsidy_budget_state.json}"
SUBSIDY_WARN_RATIO="${SUBSIDY_WARN_RATIO:-2.5}"
DEV_ADDR="${DEV_TREASURY_ADDR:-HMC-719006d93916ad52}"
SETTLEMENT_ADDR="${TREASURY_ADDR:-HMC-381c0c5e2cfcc714}"
LOG_TAG="[pool-subsidy $(date -u +%Y-%m-%dT%H:%M:%SZ)]"

COORD_TOKEN=""
for f in "${DEPLOY}/.secrets/hackme_coordinator_admin_token" "$ROOT/.secrets/hackme_coordinator_admin_token"; do
  if [[ -r "$f" ]]; then
    COORD_TOKEN="$(tr -d '\r\n' <"$f")"
    break
  fi
done

metrics="$(curl -fsS --max-time 10 "${CHAIN_BASE}/api/metrics" 2>/dev/null || echo '{}')"
coord="$(curl -fsS --max-time 10 ${COORD_TOKEN:+-H "X-Hackme-Admin-Token: $COORD_TOKEN"} \
  "${COORD_URL}/api/work/stats" 2>/dev/null || echo '{}')"
settle="$(curl -fsS --max-time 10 "${CHAIN_BASE}/api/worker/settlement" 2>/dev/null || echo '{}')"
dev_bal="$(curl -fsS --max-time 8 "${CHAIN_BASE}/api/address/${DEV_ADDR}" | jq -r '.balance_hmc // 0' 2>/dev/null || echo 0)"
settle_bal="$(curl -fsS --max-time 8 "${CHAIN_BASE}/api/address/${SETTLEMENT_ADDR}" | jq -r '.balance_hmc // 0' 2>/dev/null || echo 0)"

now="$(date -u +%s)"
emission_1h="$(printf '%s' "$metrics" | jq -r '.econ_window_total_hmc // 0' 2>/dev/null || echo 0)"
emission_expected_h="$(printf '%s' "$metrics" | jq -r '.econ_expected_empty_hmc_hour // 0' 2>/dev/null || echo 0)"
total_payout="$(printf '%s' "$coord" | jq -r '.total_payout_hmc // 0' 2>/dev/null || echo 0)"
fleet_unpaid="$(printf '%s' "$settle" | jq -r '.fleet_unpaid_hmc // .total_unpaid_hmc // 0' 2>/dev/null || echo 0)"
scheduler="$(printf '%s' "$coord" | jq -r '.scheduler_mode // "unknown"' 2>/dev/null || echo unknown)"
orders_active="$(printf '%s' "$coord" | jq -r '.orders_active // false' 2>/dev/null || echo false)"
pool_gh="$(printf '%s' "$coord" | jq -r '.pool_hashrate_gh_s // 0' 2>/dev/null || echo 0)"
reward_pm="$(printf '%s' "$coord" | jq -r '.reward_per_m // 0' 2>/dev/null || echo 0)"

genesis_topup_24h="0"
if [[ -f "$HACKME_DB" ]] && command -v sqlite3 >/dev/null; then
  genesis_topup_24h="$(sqlite3 "$HACKME_DB" "
SELECT COALESCE(ROUND(SUM(amount_units)/100000000.0, 4), 0)
FROM tx_history
WHERE from_address='${DEV_ADDR}' AND to_address='${SETTLEMENT_ADDR}'
  AND status='included' AND applied_at >= strftime('%s','now') - 86400;
" 2>/dev/null || echo 0)"
fi

mkdir -p "$(dirname "$STATE_FILE")"
prev_ts="0"
prev_payout="0"
if [[ -f "$STATE_FILE" ]]; then
  prev_ts="$(jq -r '.ts // 0' "$STATE_FILE" 2>/dev/null || echo 0)"
  prev_payout="$(jq -r '.total_payout_hmc // 0' "$STATE_FILE" 2>/dev/null || echo 0)"
fi

report="$(python3 - "$now" "$prev_ts" "$prev_payout" "$total_payout" "$emission_1h" \
  "$emission_expected_h" "$fleet_unpaid" "$genesis_topup_24h" "$dev_bal" "$settle_bal" \
  "$scheduler" "$orders_active" "$pool_gh" "$reward_pm" "$SUBSIDY_WARN_RATIO" <<'PY'
import json, sys
now, prev_ts, prev_pay, pay, em1h, em_exp, unpaid, top24, dev, stl = map(float, sys.argv[1:11])
scheduler, orders_active, pool_gh, rpm, warn_ratio = sys.argv[11], sys.argv[12], float(sys.argv[13]), float(sys.argv[14]), float(sys.argv[15])
accrual_h = 0.0
dt_h = 0.0
if prev_ts > 0 and now > prev_ts and pay >= prev_pay:
    dt_h = (now - prev_ts) / 3600.0
    if dt_h >= 0.05:
        accrual_h = (pay - prev_pay) / dt_h
em_ref = em1h if em1h > 0 else em_exp
subsidy_h = max(0.0, accrual_h - em_ref) if accrual_h > 0 and em_ref > 0 else 0.0
ratio = (accrual_h / em_ref) if accrual_h > 0 and em_ref > 0 else 0.0
warn = ratio > warn_ratio and scheduler == "baseline"
out = {
    "ts": int(now),
    "scheduler_mode": scheduler,
    "orders_active": orders_active in ("true", "True", "1"),
    "pool_hashrate_gh_s": pool_gh,
    "reward_per_m": rpm,
    "emission_window_1h_hmc": em1h,
    "emission_expected_h_hmc": em_exp,
    "accrual_est_h_hmc": round(accrual_h, 4),
    "accrual_sample_hours": round(dt_h, 3),
    "subsidy_gap_h_hmc": round(subsidy_h, 4),
    "subsidy_ratio": round(ratio, 2),
    "fleet_unpaid_hmc": unpaid,
    "genesis_topup_24h_hmc": top24,
    "dev_treasury_hmc": dev,
    "settlement_float_hmc": stl,
    "total_payout_hmc": pay,
    "subsidy_warn": warn,
}
print(json.dumps(out, indent=2))
PY
)"

printf '%s\n' "$report" | tee "${STATE_FILE}.latest.json" >/dev/null
jq -n --argjson snap "$report" --argjson prev "$(cat "$STATE_FILE" 2>/dev/null || echo '{}')" \
  '$snap + {prev_ts: ($prev.ts // 0)}' >"$STATE_FILE" 2>/dev/null || printf '%s\n' "$report" >"$STATE_FILE"

echo "$LOG_TAG scheduler=${scheduler} orders_active=${orders_active} pool_gh=${pool_gh}"
echo "$LOG_TAG emission_1h=${emission_1h} expected_h=${emission_expected_h} accrual_est_h=$(echo "$report" | jq -r '.accrual_est_h_hmc')"
echo "$LOG_TAG subsidy_ratio=$(echo "$report" | jq -r '.subsidy_ratio') gap_h=$(echo "$report" | jq -r '.subsidy_gap_h_hmc') fleet_unpaid=${fleet_unpaid}"
echo "$LOG_TAG genesis_topup_24h=${genesis_topup_24h} dev=${dev_bal} settlement=${settle_bal}"

if [[ "$(echo "$report" | jq -r '.subsidy_warn // false')" == "true" ]]; then
  echo "$LOG_TAG WARN baseline accrual exceeds emission — treasury subsidy active (ratio > ${SUBSIDY_WARN_RATIO})" >&2
  exit 2
fi
exit 0
