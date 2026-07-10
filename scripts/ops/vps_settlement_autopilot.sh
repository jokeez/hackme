#!/usr/bin/env bash
# Hub ops autopilot: token sync, treasury float, disk/WAL guard, optional settle nudge.
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
    treasury_bootstrap_guard.sh sync_settlement_admin_token.sh vps_disk_wal_guard.sh settle_worker_payouts.sh; do
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
export TREASURY_ADDR="${TREASURY_ADDR:-HMC-381c0c5e2cfcc714}"
export TREASURY_FUND_SEED_HEX="${TREASURY_FUND_SEED_HEX:-${DEPLOY}/.secrets/hackme_treasury_ed25519_seed.hex}"
export MIN_FLOAT_HMC="${MIN_FLOAT_HMC:-20}"
export TOPUP_HMC="${TOPUP_HMC:-30}"
export MAX_GENESIS_TOPUP_24H_HMC="${MAX_GENESIS_TOPUP_24H_HMC:-25}"
export CATCHUP_TOPUP_HMC="${CATCHUP_TOPUP_HMC:-180}"
export HACKME_ADMIN_TOKEN=""
if [[ -f "${DEPLOY}/.env.vps" ]]; then
  # shellcheck disable=SC1090
  set -a && source "${DEPLOY}/.env.vps" && set +a
  export HACKME_ADMIN_TOKEN
fi

echo "$LOG_TAG start"

bash "${DEPLOY}/scripts/ops/vps_disk_wal_guard.sh" || echo "$LOG_TAG WARN disk guard"

bash "${DEPLOY}/scripts/ops/sync_settlement_admin_token.sh" || echo "$LOG_TAG WARN token sync"

if bash "${DEPLOY}/scripts/ops/ensure_settlement_treasury_float.sh"; then
  :
else
  echo "$LOG_TAG WARN treasury float"
fi

HACKME_DB="${HACKME_DB:-${DEPLOY}/data/hackme.db}" \
  MAX_GENESIS_TOPUP_24H_HMC="${MAX_GENESIS_TOPUP_24H_HMC}" \
  bash "${DEPLOY}/scripts/ops/treasury_bootstrap_guard.sh" \
  || echo "$LOG_TAG WARN treasury bootstrap guard"

# Nudge settlement if treasury healthy and coordinator has material unpaid accrual.
treasury="$(curl -fsS --max-time 8 "${CHAIN_BASE}/api/address/${TREASURY_ADDR}" | jq -r '.balance_hmc // 0')"
if python3 -c "import sys; sys.exit(0 if float(sys.argv[1])>=10 else 1)" "$treasury" 2>/dev/null; then
  if [[ -f "${DEPLOY}/.env.settlement" ]]; then
    # shellcheck disable=SC1090
    set -a && source "${DEPLOY}/.env.settlement" && set +a
  fi
  export ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
  if [[ -n "${ADMIN_TOKEN:-}" ]]; then
  set +e
  sudo -u hackme env ADMIN_TOKEN="$ADMIN_TOKEN" \
    COORD_ADMIN_TOKEN="$(tr -d '\r\n' <"${DEPLOY}/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)" \
    bash "${DEPLOY}/scripts/ops/settle_worker_payouts.sh" 2>&1 | tail -5
  set -e
  fi
fi

echo "$LOG_TAG treasury=${treasury} HMC done"
