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
# Default OFF: no genesis→settlement top-ups while economy is redesigned (override with =0).
export DISABLE_TREASURY_TOPUP="${DISABLE_TREASURY_TOPUP:-1}"
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
# Settler spends TREASURY_ADDR (hub: primary node wallet). Never treat legacy float as spendable cover.
SETTLEMENT_PRIMARY="${SETTLEMENT_PRIMARY:-${TREASURY_ADDR}}"
primary_bal="$(curl -fsS --max-time 8 "${CHAIN_BASE}/api/address/${SETTLEMENT_PRIMARY}" | jq -r '.balance_hmc // 0' 2>/dev/null || echo 0)"

if python3 -c "import sys; sys.exit(0 if float(sys.argv[1])<5 and float(sys.argv[2])>10 else 1)" \
  "$primary_bal" "$fleet_unpaid" 2>/dev/null; then
  echo "$LOG_TAG ALERT primary=${primary_bal} HMC fleet_unpaid=${fleet_unpaid} HMC — settler_payer_shortfall" >&2
fi

if bash "${DEPLOY}/scripts/ops/ensure_settlement_treasury_float.sh"; then
  :
else
  echo "$LOG_TAG WARN treasury float"
fi

treasury="$(curl -fsS --max-time 8 "${CHAIN_BASE}/api/address/${TREASURY_ADDR}" | jq -r '.balance_hmc // 0' 2>/dev/null || echo "$treasury")"
primary_bal="$(curl -fsS --max-time 8 "${CHAIN_BASE}/api/address/${SETTLEMENT_PRIMARY}" | jq -r '.balance_hmc // 0' 2>/dev/null || echo "$primary_bal")"

HACKME_DB="${HACKME_DB:-${DEPLOY}/data/hackme.db}" \
  MAX_GENESIS_TOPUP_24H_HMC="${MAX_GENESIS_TOPUP_24H_HMC}" \
  FLEET_UNPAID_HMC="$fleet_unpaid" \
  SETTLEMENT_BALANCE_HMC="$primary_bal" \
  bash "${DEPLOY}/scripts/ops/treasury_bootstrap_guard.sh" \
  || echo "$LOG_TAG WARN treasury bootstrap guard"

# Nudge settlement only when primary can cover a meaningful chunk (not legacy float).
settle_min="$(python3 -c "print(max(1.0, float('${MIN_FLOAT_HMC}')*0.5))")"
chunk_need="$(python3 -c "import sys; u=float(sys.argv[1]); m=float(sys.argv[2]); print(min(u,m) if u>0 else m)" "$fleet_unpaid" "$settle_min")"
if python3 -c "import sys; sys.exit(0 if float(sys.argv[1])+1e-12 < float(sys.argv[2]) else 1)" \
  "$primary_bal" "$chunk_need" 2>/dev/null; then
  echo "$LOG_TAG SKIP settle: primary=${primary_bal} < need=${chunk_need} (unpaid=${fleet_unpaid}) — heal primary float first" >&2
elif python3 -c "import sys; sys.exit(0 if float(sys.argv[1])>=float(sys.argv[2]) else 1)" "$primary_bal" "$settle_min" 2>/dev/null; then
  export ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
  if [[ -n "${ADMIN_TOKEN:-}" ]]; then
  set +e
  # Env file (not argv): sudo/journal must not log settle tokens on the command line.
  _settle_env="$(mktemp "${TMPDIR:-/tmp}/hackme-settle-env.XXXXXX")"
  {
    printf 'ADMIN_TOKEN=%s\n' "${ADMIN_TOKEN}"
    printf 'COORD_ADMIN_TOKEN=%s\n' "$(tr -d '\r\n' <"${DEPLOY}/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)"
    printf 'WORKER_PAYOUT_MAP=%s\n' "${WORKER_PAYOUT_MAP:-}"
    printf 'MIN_SETTLE_HMC=%s\n' "${MIN_SETTLE_HMC:-}"
    printf 'MAX_SETTLE_HMC=%s\n' "${MAX_SETTLE_HMC:-}"
    printf 'CLEAR_PENDING_HMC=%s\n' "${CLEAR_PENDING_HMC:-0}"
    # FORCE only when a stamped/ambiguous pending is known-rejected on chain.
    printf 'CLEAR_PENDING_HMC_FORCE=%s\n' "${CLEAR_PENDING_HMC_FORCE:-0}"
  } >"${_settle_env}"
  chmod 600 "${_settle_env}"
  chown hackme:hackme "${_settle_env}" 2>/dev/null || true
  sudo -u hackme bash -c 'set -a; # shellcheck disable=SC1090
    source "$1"; set +a; shift; exec bash "$@"' bash "${_settle_env}" \
    "${DEPLOY}/scripts/ops/settle_worker_payouts.sh" 2>&1 | tail -8
  rm -f "${_settle_env}"
  set -e
  fi
fi

echo "$LOG_TAG primary=${primary_bal} treasury=${treasury} HMC fleet_unpaid=${fleet_unpaid} HMC done"
