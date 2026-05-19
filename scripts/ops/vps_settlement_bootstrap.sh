#!/usr/bin/env bash
# One-shot: sync admin token, repair state, run settlement, healthcheck on VPS.
#   NODE_SSH=hackme-vps bash scripts/ops/vps_settlement_bootstrap.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
export NODE_SSH

echo "[bootstrap] rsync settlement scripts"
for f in sync_settlement_admin_token.sh repair_worker_settlement_state.sh \
  settle_worker_payouts.sh settlement_healthcheck.sh settlement.env.example; do
  rsync -avz "$ROOT/scripts/ops/$f" "$NODE_SSH:${NODE_DEPLOY_DIR:-/opt/hackme}/scripts/ops/"
done

echo "[bootstrap] sync ADMIN_TOKEN"
bash "$ROOT/scripts/ops/sync_settlement_admin_token.sh"

echo "[bootstrap] repair state"
bash "$ROOT/scripts/ops/repair_worker_settlement_state.sh"

echo "[bootstrap] run settlement"
ssh -o BatchMode=yes "$NODE_SSH" "cd '${NODE_DEPLOY_DIR:-/opt/hackme}' && set -a && . ./.env.settlement && set +a && bash scripts/ops/settle_worker_payouts.sh"

echo "[bootstrap] healthcheck (on VPS)"
ssh -o BatchMode=yes "$NODE_SSH" "cd '${NODE_DEPLOY_DIR:-/opt/hackme}' && set -a && . ./.env.settlement && set +a && \
  LOCAL_BASE=http://127.0.0.1:18080 bash scripts/ops/settlement_healthcheck.sh" || true

echo "[bootstrap] done"
