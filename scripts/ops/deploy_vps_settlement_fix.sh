#!/usr/bin/env bash
# Fix VPS settlement: correct WORKER_PAYOUT_MAP, repair over-counted state, deploy settle script, run payout.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
WALLET="${WALLET:-HMC-91fe007e4036c602}"
WORKER_ID="${WORKER_ID:-worker-kapa-pc}"
# Default: route both your rigs to one wallet (override with WORKER_PAYOUT_MAP env).
PAYOUT_MAP="${WORKER_PAYOUT_MAP:-worker-kapa-pc=${WALLET},worker-vps-msk-01=${WALLET}}"

echo "[settle-fix] nginx coordinator query string (workers{} on HTTPS)"
if [[ -f "$ROOT/scripts/ops/vps_patch_coordinator_nginx_query.sh" ]]; then
  scp "$ROOT/scripts/ops/vps_patch_coordinator_nginx_query.sh" "$NODE_SSH:/tmp/"
  ssh "$NODE_SSH" "sudo bash /tmp/vps_patch_coordinator_nginx_query.sh" || true
fi

echo "[settle-fix] rsync settlement ops scripts"
for f in settle_worker_payouts.sh sync_settlement_admin_token.sh repair_worker_settlement_state.sh settlement_healthcheck.sh; do
  rsync -avz "$ROOT/scripts/ops/$f" "$NODE_SSH:$NODE_DEPLOY_DIR/scripts/ops/"
done

echo "[settle-fix] sync ADMIN_TOKEN with node"
NODE_SSH="$NODE_SSH" NODE_DEPLOY_DIR="$NODE_DEPLOY_DIR" bash "$ROOT/scripts/ops/sync_settlement_admin_token.sh"

echo "[settle-fix] WORKER_PAYOUT_MAP=${PAYOUT_MAP}"
ssh "$NODE_SSH" "grep -q '^WORKER_PAYOUT_MAP=' '$NODE_DEPLOY_DIR/.env.settlement' && \
  sed -i 's|^WORKER_PAYOUT_MAP=.*|WORKER_PAYOUT_MAP=${PAYOUT_MAP}|' '$NODE_DEPLOY_DIR/.env.settlement' || \
  echo 'WORKER_PAYOUT_MAP=${PAYOUT_MAP}' >>'$NODE_DEPLOY_DIR/.env.settlement'"

echo "[settle-fix] repair over-counted settlement state"
NODE_SSH="$NODE_SSH" NODE_DEPLOY_DIR="$NODE_DEPLOY_DIR" bash "$ROOT/scripts/ops/repair_worker_settlement_state.sh"

echo "[settle-fix] run settle_worker_payouts.sh"
ssh "$NODE_SSH" "cd '$NODE_DEPLOY_DIR' && set -a && . ./.env.settlement && set +a && bash scripts/ops/settle_worker_payouts.sh"

echo "[settle-fix] coordinator worker row:"
ssh "$NODE_SSH" 'TOKEN=$(tr -d "\r\n" <'"$NODE_DEPLOY_DIR"'/.secrets/hackme_coordinator_admin_token); curl -fsS -H "X-Hackme-Admin-Token: $TOKEN" http://127.0.0.1:18081/api/work/stats?details=1 | jq '"'"'.workers["'"$WORKER_ID"'"]'"'"''
