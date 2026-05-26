#!/usr/bin/env bash
# Production finalize: deploy fair pool, settle, prune ghost workers, ideal local miner, audits.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
NODE_SSH="${NODE_SSH:-hackme-vps}"
COORD="${COORD:-https://hackme.tech/pool/coordinator}"
WALLET="${WALLET:-HMC-91fe007e4036c602}"
ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)"

log() { echo "[pool-finalize] $*"; }

log "1/6 deploy fair pool + settlement on $NODE_SSH"
NODE_SSH="$NODE_SSH" WALLET="$WALLET" bash "$ROOT/scripts/ops/apply_miner_fair_pool.sh" 2>&1 | tail -30

log "2/6 prune offline ghost workers on coordinator"
for prefix in worker-kapa-fair- worker-kapa-rig-; do
  curl -fsS -X POST "${COORD}/api/work/admin/prune-workers" \
    -H "X-Hackme-Admin-Token: ${ADMIN}" \
    -H "Content-Type: application/json" \
    -d "{\"prefix\":\"${prefix}\",\"stale_sec\":300,\"ignore_payout\":true,\"dry_run\":false}" \
    2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('prefix'),'removed',len(d.get('removed') or []))" || true
done

log "3/6 local ideal miner (single worker-kapa-pc)"
bash "$ROOT/scripts/ops/start_local_ideal_miner.sh"

log "4/6 pool fairness sample"
SAMPLES=3 INTERVAL_SEC=10 COORD_URL="$COORD" bash "$ROOT/scripts/ops/pool_fairness_audit.sh" 2>&1 | tail -25

log "5/6 miner happiness"
bash "$ROOT/scripts/ops/miner_happiness_check.sh" 2>&1 | tail -25

log "6/6 order economics (recent fair order)"
ssh -o BatchMode=yes -i "${HOME}/.ssh/id_ed25519}" "$NODE_SSH" \
  'python3 /opt/hackme/scripts/ops/order_economics_audit.py /opt/hackme/data/hackme.db order-audit-1779729050' 2>/dev/null | tail -12

log "done — wallet $WALLET"
