#!/usr/bin/env bash
# Cron gate: settle dust accruals then prune offline coordinator worker rows.
#
#   bash scripts/ops/prune_offline_pool_workers_gate.sh
#   NODE_SSH=hackme-vps bash scripts/ops/prune_offline_pool_workers_gate.sh   # remote settle + local prune API
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
STALE_SEC="${STALE_SEC:-1800}"
MAX_PAYOUT_HMC="${MAX_PAYOUT_HMC:-2}"
NODE_SSH="${NODE_SSH:-}"

log() { echo "[prune-offline-gate $(date -u +%H:%M:%S)] $*"; }

if [[ -n "$NODE_SSH" ]]; then
  log "remote settlement nudge on $NODE_SSH"
  ssh -o BatchMode=yes "$NODE_SSH" "cd /opt/hackme && set -a && source .env.settlement 2>/dev/null && set +a && \
    ADMIN_TOKEN=\$(tr -d '\\r\\n' </opt/hackme/.secrets/hackme_admin_token 2>/dev/null || true) \
    COORD_ADMIN_TOKEN=\$(tr -d '\\r\\n' </opt/hackme/.secrets/hackme_coordinator_admin_token 2>/dev/null || true) \
    FORCE_SETTLE_ALL=1 MIN_SETTLE_HMC=0.0001 bash scripts/ops/settle_worker_payouts.sh" 2>&1 | tail -8 || log "WARN remote settle"
fi

log "prune offline stale workers (max_payout=${MAX_PAYOUT_HMC} stale=${STALE_SEC})"
STALE_SEC="$STALE_SEC" MAX_PAYOUT_HMC="$MAX_PAYOUT_HMC" COORD_URL="$COORD_URL" \
  bash "$ROOT/scripts/ops/purge_stale_pool_workers.sh"

for prefix in worker-kapa-fair- worker-kapa-rig- worker-stress- worker-crypto-matrix-; do
  log "prune test prefix ${prefix}"
  PREFIX="$prefix" STALE_SEC=300 IGNORE_PAYOUT=1 \
    bash "$ROOT/scripts/ops/purge_stale_pool_workers.sh" || true
done

log "done"
