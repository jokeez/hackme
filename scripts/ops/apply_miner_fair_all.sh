#!/usr/bin/env bash
# End-to-end miner fairness: VPS pool + settlement + MSK + desktop remote worker + toolchains.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

WALLET="${WALLET:-HMC-91fe007e4036c602}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
MSK_SSH="${MSK_SSH:-root@82.146.53.7}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
MSK_DEPLOY="${MSK_DEPLOY_DIR:-/opt/hackme-worker}"

log() { echo "[miner-fair-all] $*"; }

log "[1/6] VPS pool + coordinator fair settings"
bash "$ROOT/scripts/ops/apply_miner_fair_pool.sh"

log "[2/6] VPS from_code toolchains (hackme user)"
ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" "sudo bash '$DEPLOY/scripts/ops/install_vps_from_code_toolchains.sh' 2>&1 | tail -25" || {
  log "WARN: toolchain install incomplete — retry manually on VPS"
}

log "[3/6] nginx coordinator timeouts + settlement health"
ssh -o BatchMode=yes "$NODE_SSH" "bash -s" <<REMOTE
set -euo pipefail
DEPLOY='$DEPLOY'
NGINX="/etc/nginx/sites-available/hackme-site-domain.conf"
if [[ -f "\$DEPLOY/scripts/ops/vps_patch_coordinator_nginx_query.sh" ]]; then
  sudo bash "\$DEPLOY/scripts/ops/vps_patch_coordinator_nginx_query.sh" || true
fi
if [[ -f "\$NGINX" ]]; then
  sudo sed -i 's/proxy_read_timeout 120s/proxy_read_timeout 180s/g; s/proxy_send_timeout 120s/proxy_send_timeout 180s/g' "\$NGINX" 2>/dev/null || true
  grep -q 'proxy_read_timeout 180s' "\$NGINX" 2>/dev/null || \
    sudo sed -i '/pool\\/coordinator/a\        proxy_read_timeout 180s;\n        proxy_send_timeout 180s;' "\$NGINX" 2>/dev/null || true
  sudo nginx -t && sudo systemctl reload nginx
fi
ENV_SETTLE="\$DEPLOY/.env.settlement"
grep -q '^SETTLE_TX_WAIT_SEC=' "\$ENV_SETTLE" 2>/dev/null && \
  sudo sed -i 's/^SETTLE_TX_WAIT_SEC=.*/SETTLE_TX_WAIT_SEC=90/' "\$ENV_SETTLE" || \
  echo 'SETTLE_TX_WAIT_SEC=90' | sudo tee -a "\$ENV_SETTLE" >/dev/null
grep -q '^SETTLE_PAYOUT_PAUSE_SEC=' "\$ENV_SETTLE" 2>/dev/null && \
  sudo sed -i 's/^SETTLE_PAYOUT_PAUSE_SEC=.*/SETTLE_PAYOUT_PAUSE_SEC=10/' "\$ENV_SETTLE" || \
  echo 'SETTLE_PAYOUT_PAUSE_SEC=10' | sudo tee -a "\$ENV_SETTLE" >/dev/null
grep -q '^SETTLE_NONCE_RETRIES=' "\$ENV_SETTLE" 2>/dev/null && \
  sudo sed -i 's/^SETTLE_NONCE_RETRIES=.*/SETTLE_NONCE_RETRIES=6/' "\$ENV_SETTLE" || \
  echo 'SETTLE_NONCE_RETRIES=6' | sudo tee -a "\$ENV_SETTLE" >/dev/null
bash "\$DEPLOY/scripts/ops/sync_settlement_admin_token.sh" 2>/dev/null || true
ADMIN=\$(grep '^HACKME_ADMIN_TOKEN=' "\$DEPLOY/.env.vps" | cut -d= -f2-)
COORD=\$(tr -d '\r\n' <"\$DEPLOY/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)
CHAIN_BASE=http://127.0.0.1:18080 COORD_URL=http://127.0.0.1:18081 \
  ADMIN_TOKEN="\$ADMIN" COORD_ADMIN_TOKEN="\$COORD" \
  bash "\$DEPLOY/scripts/ops/settlement_healthcheck.sh" || true
REMOTE

log "[4/6] MSK worker — remote fair batch + timeouts"
if ssh -o BatchMode=yes -o ConnectTimeout=8 "$MSK_SSH" true 2>/dev/null; then
  COORD_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)"
  SEED="$(tr -d '\r\n' <"$ROOT/data/miner_submit_ed25519_seed.hex" 2>/dev/null || true)"
  ssh -o BatchMode=yes "$MSK_SSH" "bash -s" <<REMOTE
set -euo pipefail
DEPLOY='$MSK_DEPLOY'
mkdir -p "\$DEPLOY/logs"
cat >"\$DEPLOY/.env.worker" <<ENV
COORD_URL=https://hackme.tech/pool/coordinator
COORD_TOKEN=${COORD_TOKEN}
COORD_ADMIN_TOKEN=${COORD_TOKEN}
WORKER_ID=worker-vps-msk-01
HACKME_MINER_ED25519_SEED_HEX=${SEED}
BATCH_SIZE=1048576
HACKME_GPU_DISABLE=1
HACKME_WORKER_CLAIM_TIMEOUT=90s
HACKME_WORKER_SUBMIT_TIMEOUT=120s
HACKME_WORKER_CLAIM_COOLDOWN_MS=800
PAYOUT_ADDRESS=${WALLET}
ENV
chmod 600 "\$DEPLOY/.env.worker"
if [[ -f /etc/systemd/system/hackme-worker.service ]]; then
  sed -i 's|-batch [0-9]*|-batch 1048576|g' /etc/systemd/system/hackme-worker.service 2>/dev/null || true
  systemctl daemon-reload
  systemctl restart hackme-worker
  sleep 3
  systemctl is-active hackme-worker
fi
REMOTE
else
  log "WARN: MSK SSH unavailable — run: bash scripts/ops/repair_msk_worker.sh"
fi

log "[5/6] desktop .env.desktop remote worker tuning"
DESKTOP_ENV="$ROOT/.env.desktop"
if [[ -f "$DESKTOP_ENV" ]]; then
  set_kv() {
    local k="$1" v="$2"
    if grep -q "^${k}=" "$DESKTOP_ENV" 2>/dev/null; then
      sed -i "s|^${k}=.*|${k}=${v}|" "$DESKTOP_ENV"
    else
      echo "${k}=${v}" >>"$DESKTOP_ENV"
    fi
  }
  set_kv HACKME_WORKER_BATCH_SIZE 1048576
  set_kv HACKME_WORKER_CLAIM_TIMEOUT 90s
  set_kv HACKME_WORKER_SUBMIT_TIMEOUT 120s
  set_kv WORKER_PAYOUT_MAP "worker-kapa-pc=${WALLET}"
  log "patched $DESKTOP_ENV"
fi

log "[6/6] restart desktop worker + happiness check"
if curl -fsS --max-time 3 http://127.0.0.1:8080/api/status >/dev/null 2>&1; then
  go build -tags opencl -o "$ROOT/logs/desktop/hackme-node-desktop" . 2>/dev/null || true
  # Restart node if pid file exists so handleWorkerStart picks up new remote defaults
  if [[ -f "$ROOT/logs/desktop/node.pid" ]]; then
    pid="$(cat "$ROOT/logs/desktop/node.pid" 2>/dev/null || true)"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      sleep 2
    fi
  fi
  if [[ -x "$ROOT/scripts/ops/desktop_mode_up.sh" ]]; then
    DESKTOP_PROFILE=worker SKIP_TOOLCHAINS=1 bash "$ROOT/scripts/ops/desktop_mode_up.sh" >/dev/null 2>&1 || true
  fi
  bash "$ROOT/scripts/ops/desktop_worker_reset.sh" 2>&1 | tail -15 || true
else
  log "desktop node not running — start with desktop_mode_up.sh then desktop_worker_reset.sh"
fi

sleep 20
bash "$ROOT/scripts/ops/miner_happiness_check.sh" 2>&1 | tail -40

log "done"
