#!/usr/bin/env bash
# Ideal pool deploy: build, VPS binaries, settlement hardening, payout map, smoke.
#   NODE_SSH=hackme-vps WALLET=HMC-91fe007e4036c602 bash scripts/ops/pool_ideal_deploy.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
WALLET="${WALLET:-HMC-91fe007e4036c602}"
PAYOUT_MAP="${WORKER_PAYOUT_MAP:-worker-kapa-pc=${WALLET},worker-vps-msk-01=${WALLET},vps-canary-01=${WALLET}}"

echo "[ideal-deploy] build coordinator + node"
(cd "$ROOT" && go build -o /tmp/hackme-coordinator ./cmd/coordinator/ && go build -o /tmp/hackme-node .)

echo "[ideal-deploy] rsync binaries + ops"
rsync -avz /tmp/hackme-coordinator /tmp/hackme-node \
  "$ROOT/scripts/ops/vps_patch_coordinator_nginx_query.sh" \
  "$ROOT/scripts/ops/settle_worker_payouts.sh" \
  "$ROOT/scripts/ops/sync_settlement_admin_token.sh" \
  "$ROOT/scripts/ops/repair_worker_settlement_state.sh" \
  "$ROOT/scripts/ops/systemd/hackme-worker-settlement.service" \
  "$ROOT/scripts/ops/systemd/hackme-worker-settlement.timer" \
  "$NODE_SSH:$DEPLOY/"
rsync -avz "$ROOT/scripts/ops/systemd/hackme-worker-settlement.service" \
  "$ROOT/scripts/ops/systemd/hackme-worker-settlement.timer" \
  "$NODE_SSH:/tmp/"

ssh -o BatchMode=yes "$NODE_SSH" "bash -s" <<REMOTE
set -euo pipefail
DEPLOY='$DEPLOY'
WALLET='$WALLET'
PAYOUT_MAP='$PAYOUT_MAP'
ENV="\$DEPLOY/.env.settlement"
touch "\$ENV"
grep -q '^WORKER_PAYOUT_MAP=' "\$ENV" && \
  sed -i "s|^WORKER_PAYOUT_MAP=.*|WORKER_PAYOUT_MAP=\${PAYOUT_MAP}|" "\$ENV" || \
  echo "WORKER_PAYOUT_MAP=\${PAYOUT_MAP}" >>"\$ENV"
for kv in \
  "MIN_SETTLE_HMC=0.0001" \
  "DAILY_MIN_SETTLE_HMC=0.0001" \
  "SETTLE_SEQUENTIAL=1" \
  "SETTLE_TX_WAIT_SEC=45" \
  "STATE_FILE=\$DEPLOY/data/worker_settlement_state.json"; do
  key="\${kv%%=*}"; val="\${kv#*=}"
  grep -q "^\${key}=" "\$ENV" && sed -i "s|^\${key}=.*|\${key}=\${val}|" "\$ENV" || echo "\${key}=\${val}" >>"\$ENV"
done
sudo systemctl stop hackme-coordinator
sudo cp "\$DEPLOY/hackme-coordinator" "\$DEPLOY/coordinator"
sudo chmod 755 "\$DEPLOY/coordinator" "\$DEPLOY/hackme-node"
if [[ -f "\$DEPLOY/scripts/ops/vps_patch_coordinator_nginx_query.sh" ]]; then
  sudo bash "\$DEPLOY/scripts/ops/vps_patch_coordinator_nginx_query.sh" || true
fi
NGINX="/etc/nginx/sites-available/hackme-site-domain.conf"
if [[ -f "\$NGINX" ]]; then
  sudo sed -i 's/proxy_read_timeout 120s/proxy_read_timeout 180s/g; s/proxy_send_timeout 120s/proxy_send_timeout 180s/g' "\$NGINX"
  sudo grep -q proxy_buffering "\$NGINX" || \
    sudo sed -i '/proxy_send_timeout/a\\        proxy_buffering off;\\n        proxy_request_buffering off;' "\$NGINX"
  sudo nginx -t && sudo systemctl reload nginx
fi
COORD_UNIT=/etc/systemd/system/hackme-coordinator.service
if [[ -f "\$COORD_UNIT" ]]; then
  sudo grep -q 'HACKME_COORDINATOR_WRITE_TIMEOUT_SEC' "\$COORD_UNIT" || \
    sudo sed -i '/^EnvironmentFile=/a Environment=HACKME_COORDINATOR_WRITE_TIMEOUT_SEC=120' "\$COORD_UNIT"
  sudo grep -q 'HACKME_COORDINATOR_READ_TIMEOUT_SEC' "\$COORD_UNIT" || \
    sudo sed -i '/^EnvironmentFile=/a Environment=HACKME_COORDINATOR_READ_TIMEOUT_SEC=60' "\$COORD_UNIT"
fi
sudo cp /tmp/hackme-worker-settlement.service /etc/systemd/system/
sudo cp /tmp/hackme-worker-settlement.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable hackme-worker-settlement.timer
sudo systemctl restart hackme-coordinator hackme-node
sudo chown hackme:hackme "\$DEPLOY/data/worker_settlement_state.json" "\$DEPLOY/data/worker_settlement_state.json.flock" 2>/dev/null || true
bash "\$DEPLOY/scripts/ops/sync_settlement_admin_token.sh"
bash "\$DEPLOY/scripts/ops/repair_worker_settlement_state.sh"
systemctl start hackme-worker-settlement.service || true
echo "[ideal-deploy] services:" \$(systemctl is-active hackme-node hackme-coordinator hackme-workerpoh hackme-worker-settlement.timer)
curl -fsS http://127.0.0.1:18081/api/pool/stats | head -c 200
echo
curl -fsS http://127.0.0.1:18080/api/global/metrics | python3 -c "import sys,json; d=json.load(sys.stdin); n=d['network']; print('pool_gh', round(n.get('pool_hashrate_gh_s',0),2), 'rigs', len(n.get('active_rigs',[])))"
REMOTE

echo "[ideal-deploy] done"
