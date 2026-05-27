#!/usr/bin/env bash
# VPS: genesis SUP, enable on-chain flag on coordinator, run settlement, smoke wallet.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"

echo "[vps-sup] patch coordinator env"
ssh "$NODE_SSH" "ENV='$DEPLOY/.env.coord'; touch \"\$ENV\"; grep -q '^HACKME_SUP_ON_CHAIN_SETTLE=' \"\$ENV\" || echo 'HACKME_SUP_ON_CHAIN_SETTLE=1' >>\"\$ENV\"; grep -q '^HACKME_COORDINATOR_SUP_ENABLED=' \"\$ENV\" || echo 'HACKME_COORDINATOR_SUP_ENABLED=1' >>\"\$ENV\""

echo "[vps-sup] genesis"
ssh "$NODE_SSH" "cd '$DEPLOY' && CHAIN_BASE='$CHAIN_BASE' bash scripts/ops/sup_genesis_init.sh"

echo "[vps-sup] restart services"
ssh "$NODE_SSH" "sudo systemctl restart hackme-node hackme-coordinator 2>/dev/null || systemctl restart hackme-node hackme-coordinator"

sleep 4
echo "[vps-sup] economics"
ssh "$NODE_SSH" "curl -fsS '$CHAIN_BASE/api/sup/economics' | jq ."

echo "[vps-sup] coordinator sup_policy"
ssh "$NODE_SSH" 'TOKEN=$(tr -d "\r\n" <'"$DEPLOY"'/.secrets/hackme_coordinator_admin_token); curl -fsS -H "X-Hackme-Admin-Token: $TOKEN" '"$COORD_URL"'/api/work/stats?details=0 | jq "{total_payout_sup,sup_policy}"'

echo "[vps-sup] settle SUP"
ssh "$NODE_SSH" "cd '$DEPLOY' && CHAIN_BASE='$CHAIN_BASE' COORD_URL='$COORD_URL' MIN_SETTLE_SUP=0.0001 bash scripts/ops/settle_worker_sup.sh" || true

echo "[vps-sup] done"
