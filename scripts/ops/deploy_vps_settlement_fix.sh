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

echo "[settle-fix] rsync settle_worker_payouts.sh"
rsync -avz "$ROOT/scripts/ops/settle_worker_payouts.sh" "$NODE_SSH:$NODE_DEPLOY_DIR/scripts/ops/"

echo "[settle-fix] WORKER_PAYOUT_MAP=${PAYOUT_MAP}"
ssh "$NODE_SSH" "grep -q '^WORKER_PAYOUT_MAP=' '$NODE_DEPLOY_DIR/.env.settlement' && \
  sed -i 's|^WORKER_PAYOUT_MAP=.*|WORKER_PAYOUT_MAP=${PAYOUT_MAP}|' '$NODE_DEPLOY_DIR/.env.settlement' || \
  echo 'WORKER_PAYOUT_MAP=${PAYOUT_MAP}' >>'$NODE_DEPLOY_DIR/.env.settlement'"

echo "[settle-fix] reset worker-kapa-pc settled_hmc to last on-chain tx (~0.000816 HMC)"
ssh "$NODE_SSH" "python3 -c \"
import json
p='$NODE_DEPLOY_DIR/data/worker_settlement_state.json'
st=json.load(open(p))
w=st.get('workers',{}).get('$WORKER_ID')
if w and w.get('last_tx_hash'):
    w['settled_hmc']=0.00081565
    st['workers']['$WORKER_ID']=w
    json.dump(st, open(p,'w'), indent=2)
    print('ok settled_hmc=', w['settled_hmc'])
\""

echo "[settle-fix] run settle_worker_payouts.sh"
ssh "$NODE_SSH" "cd '$NODE_DEPLOY_DIR' && set -a && . ./.env.settlement && set +a && bash scripts/ops/settle_worker_payouts.sh"

echo "[settle-fix] coordinator worker row:"
ssh "$NODE_SSH" "curl -fsS http://127.0.0.1:18081/api/work/stats?details=1 | jq '.workers[\"$WORKER_ID\"]'"
