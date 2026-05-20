#!/usr/bin/env bash
# Fix vps-canary-01 on hub VPS: per-worker nonce, active CPU mining, restart service.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
WALLET="${WALLET:-HMC-91fe007e4036c602}"

ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" "bash -s" <<REMOTE
set -euo pipefail
DEPLOY='$DEPLOY'
ENV="\$DEPLOY/.env.worker"
mkdir -p "\$DEPLOY/logs" "\$DEPLOY/data" "\$DEPLOY/bin"
touch "\$ENV"
set_kv() {
  local k="\$1" v="\$2"
  if grep -q "^\${k}=" "\$ENV" 2>/dev/null; then
    sed -i "s|^\${k}=.*|\${k}=\${v}|" "\$ENV"
  else
    echo "\${k}=\${v}" >>"\$ENV"
  fi
}
ADMIN=\$(tr -d '\r\n' <"\$DEPLOY/.secrets/hackme_coordinator_admin_token" 2>/dev/null || grep '^HACKME_COORDINATOR_ADMIN_TOKEN=' "\$DEPLOY/.env.coord" | cut -d= -f2-)
set_kv COORD_URL http://127.0.0.1:18081
set_kv COORD_TOKEN "\$ADMIN"
set_kv COORD_ADMIN_TOKEN "\$ADMIN"
set_kv WORKER_ID vps-canary-01
set_kv BATCH_SIZE 2097152
set_kv HACKME_WORKER_CLAIM_COOLDOWN_MS 800
set_kv HACKME_WORKER_CLAIM_TIMEOUT 45s
set_kv HACKME_WORKER_SUBMIT_TIMEOUT 90s
set_kv HACKME_GPU_DISABLE 1
set_kv WORKER_BIN "\$DEPLOY/bin/workerpoh"
set_kv SKIP_WORKER_BUILD 0
set_kv FORCE_WORKER_REBUILD 1
set_kv HACKME_MINER_NONCE_FILE "\$DEPLOY/logs/miner_submit_nonce.vps-canary-01.seq"
# Fresh submit nonce above coordinator replay window
echo "\$(( \$(date +%s) * 1000 ))" >"\$DEPLOY/logs/miner_submit_nonce.vps-canary-01.seq"
chmod 600 "\$ENV" "\$DEPLOY/logs/miner_submit_nonce.vps-canary-01.seq"
cd "\$DEPLOY" && go build -trimpath -o bin/workerpoh ./cmd/workerpoh
chown hackme:hackme bin/workerpoh "\$ENV" logs/miner_submit_nonce.vps-canary-01.seq 2>/dev/null || true
systemctl restart hackme-workerpoh
sleep 4
systemctl is-active hackme-workerpoh
pgrep -af 'workerpoh.*vps-canary' | head -2 || true
tail -5 "\$(ls -t \$DEPLOY/logs/workerpoh-vps-canary-01-*.log 2>/dev/null | head -1)" 2>/dev/null || journalctl -u hackme-workerpoh -n 8 --no-pager
REMOTE

echo "[canary] done — check pool dashboard for vps-canary-01"
