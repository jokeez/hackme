#!/usr/bin/env bash
# Hub ops autopilot: token sync, treasury float, subsidy budget, disk/WAL guard, settle nudge.
#
# Cron (root or hackme):
#   */15 * * * * /opt/hackme/scripts/ops/vps_settlement_autopilot.sh >>/opt/hackme/logs/settlement-autopilot.log 2>&1
#
# From dev:
#   NODE_SSH=hackme-vps bash scripts/ops/vps_settlement_autopilot.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"

if [[ -n "$NODE_SSH" ]]; then
  for f in vps_settlement_autopilot.sh ensure_settlement_treasury_float.sh \
    treasury_bootstrap_guard.sh pool_subsidy_budget_snapshot.sh \
    sync_settlement_admin_token.sh vps_disk_wal_guard.sh settle_worker_payouts.sh; do
    rsync -az "$ROOT/scripts/ops/$f" "$NODE_SSH:$DEPLOY/scripts/ops/" 2>/dev/null || true
  done
  rsync -az "$ROOT/scripts/ops/systemd/hackme-settlement-autopilot."* "$NODE_SSH:/tmp/" 2>/dev/null || true
  ssh -o BatchMode=yes "$NODE_SSH" "sudo cp /tmp/hackme-settlement-autopilot.service /tmp/hackme-settlement-autopilot.timer /etc/systemd/system/ 2>/dev/null; sudo systemctl daemon-reload; sudo systemctl enable --now hackme-settlement-autopilot.timer 2>/dev/null; DEPLOY='$DEPLOY' bash '$DEPLOY/scripts/ops/vps_settlement_autopilot.sh'"
  exit $?
fi

DEPLOY="${DEPLOY:-/opt/hackme}"
cd "$DEPLOY"
LOG_TAG="[settlement-autopilot $(date -u +%Y-%m-%dT%H:%M:%SZ)]"

export CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
export COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
export TREASURY_ADDR="${TREASURY_ADDR:-HMC-381c0c5e2cfcc714}"
export TREASURY_FUND_SEED_HEX="${TREASURY_FUND_SEED_HEX:-${DEPLOY}/.secrets/hackme_treasury_ed25519_seed.hex}"
export MIN_FLOAT_HMC="${MIN_FLOAT_HMC:-20}"
export TOPUP_HMC="${TOPUP_HMC:-30}"
export MAX_GENESIS_TOPUP_24H_HMC="${MAX_GENESIS_TOPUP_24H_HMC:-25}"
export CATCHUP_TOPUP_HMC="${CATCHUP_TOPUP_HMC:-180}"
export CATCHUP_UNPAID_TRIGGER_HMC="${CATCHUP_UNPAID_TRIGGER_HMC:-20}"
export GENESIS_RESERVE_HMC="${GENESIS_RESERVE_HMC:-30000}"
export GENESIS_CATCHUP_RESERVE_HMC="${GENESIS_CATCHUP_RESERVE_HMC:-10000}"
export HACKME_ADMIN_TOKEN=""
if [[ -f "${DEPLOY}/.env.vps" ]]; then
  # shellcheck disable=SC1090
  set -a && source "${DEPLOY}/.env.vps" && set +a
  export HACKME_ADMIN_TOKEN
fi
if [[ -f "${DEPLOY}/.env.settlement" ]]; then
  # shellcheck disable=SC1090
  set -a && source "${DEPLOY}/.env.settlement" && set +a
fi

echo "$LOG_TAG start"

bash "${DEPLOY}/scripts/ops/vps_disk_wal_guard.sh" || echo "$LOG_TAG WARN disk guard"

bash "${DEPLOY}/scripts/ops/sync_settlement_admin_token.sh" || echo "$LOG_TAG WARN token sync"

set +e
bash "${DEPLOY}/scripts/ops/pool_subsidy_budget_snapshot.sh"
budget_rc=$?
set -e
if [[ "$budget_rc" -eq 2 ]]; then
  echo "$LOG_TAG WARN subsidy budget — baseline accrual above emission (see pool_subsidy_budget_state.json)"
fi

fleet_unpaid="$(curl -fsS --max-time 8 "${CHAIN_BASE}/api/worker/settlement" 2>/dev/null \
  | jq -r '.fleet_unpaid_hmc // .total_unpaid_hmc // 0' 2>/dev/null || echo 0)"
treasury="$(curl -fsS --max-time 8 "${CHAIN_BASE}/api/address/${TREASURY_ADDR}" | jq -r '.balance_hmc // 0' 2>/dev/null || echo 0)"

if python3 -c "import sys; sys.exit(0 if float(sys.argv[1])<5 and float(sys.argv[2])>10 else 1)" \
  "$treasury" "$fleet_unpaid" 2>/dev/null; then
  echo "$LOG_TAG ALERT treasury=${treasury} HMC fleet_unpaid=${fleet_unpaid} HMC — catch-up expected" >&2
fi

if bash "${DEPLOY}/scripts/ops/ensure_settlement_treasury_float.sh"; then
  :
else
  echo "$LOG_TAG WARN treasury float"
fi

treasury="$(curl -fsS --max-time 8 "${CHAIN_BASE}/api/address/${TREASURY_ADDR}" | jq -r '.balance_hmc // 0' 2>/dev/null || echo "$treasury")"

HACKME_DB="${HACKME_DB:-${DEPLOY}/data/hackme.db}" \
  MAX_GENESIS_TOPUP_24H_HMC="${MAX_GENESIS_TOPUP_24H_HMC}" \
  FLEET_UNPAID_HMC="$fleet_unpaid" \
  SETTLEMENT_BALANCE_HMC="$treasury" \
  bash "${DEPLOY}/scripts/ops/treasury_bootstrap_guard.sh" \
  || echo "$LOG_TAG WARN treasury bootstrap guard"

# Nudge settlement when treasury can cover meaningful payouts (half of min float).
settle_min="$(python3 -c "print(max(1.0, float('${MIN_FLOAT_HMC}')*0.5))")"
if python3 -c "import sys; sys.exit(0 if float(sys.argv[1])>=float(sys.argv[2]) else 1)" "$treasury" "$settle_min" 2>/dev/null; then
  export ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
  if [[ -n "${ADMIN_TOKEN:-}" ]]; then
  set +e
  sudo -u hackme env ADMIN_TOKEN="$ADMIN_TOKEN" \
    COORD_ADMIN_TOKEN="$(tr -d '\r\n' <"${DEPLOY}/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)" \
    bash "${DEPLOY}/scripts/ops/settle_worker_payouts.sh" 2>&1 | tail -8
  set -e
  fi
fi

echo "$LOG_TAG treasury=${treasury} HMC fleet_unpaid=${fleet_unpaid} HMC done"
