#!/usr/bin/env bash
set -euo pipefail

# Split-role rollout helper:
# - NODE VPS: canonical node + public site/explorer
# - COORD VPS: standalone coordinator
#
# This script prepares and applies a safe cutover by:
# 1) syncing code/binaries to both hosts
# 2) configuring coordinator service on COORD host
# 3) configuring node env to point to COORD host
# 4) restarting services in correct order
# 5) running smoke checks
#
# Usage example:
#   NODE_SSH="root@NODE_IP" \
#   COORD_SSH="root@COORD_IP" \
#   ADMIN_TOKEN="..." \
#   P2P_TOKEN="..." \
#   COORD_ADMIN_TOKEN="..." \
#   DOMAIN="hackme.tech" \
#   bash scripts/ops/dual_vps_cutover.sh
#
# Required:
#   NODE_SSH, COORD_SSH
#   ADMIN_TOKEN, P2P_TOKEN, COORD_ADMIN_TOKEN
#
# Optional:
#   DOMAIN                       default hackme.tech
#   NODE_MAIN_ADDR               default 0.0.0.0:18080
#   COORD_ADDR                   default 0.0.0.0:18081
#   NODE_DEPLOY_DIR              default /opt/hackme
#   COORD_DEPLOY_DIR             default /opt/hackme
#   SKIP_BUILD                   default 0 (set 1 to skip local go build)

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[dual-vps] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd ssh
require_cmd scp
require_cmd rsync
require_cmd curl
require_cmd jq
require_cmd go

NODE_SSH="${NODE_SSH:-}"
COORD_SSH="${COORD_SSH:-}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}"
COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-}"

DOMAIN="${DOMAIN:-hackme.tech}"
NODE_MAIN_ADDR="${NODE_MAIN_ADDR:-0.0.0.0:18080}"
COORD_ADDR="${COORD_ADDR:-0.0.0.0:18081}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
COORD_DEPLOY_DIR="${COORD_DEPLOY_DIR:-/opt/hackme}"
SKIP_BUILD="${SKIP_BUILD:-0}"

if [[ -z "$NODE_SSH" || -z "$COORD_SSH" ]]; then
  echo "[dual-vps] NODE_SSH and COORD_SSH are required" >&2
  exit 1
fi
if [[ -z "$ADMIN_TOKEN" || -z "$P2P_TOKEN" || -z "$COORD_ADMIN_TOKEN" ]]; then
  echo "[dual-vps] ADMIN_TOKEN, P2P_TOKEN and COORD_ADMIN_TOKEN are required" >&2
  exit 1
fi

echo "[dual-vps] NODE_SSH=$NODE_SSH"
echo "[dual-vps] COORD_SSH=$COORD_SSH"
echo "[dual-vps] DOMAIN=$DOMAIN"

if [[ "$SKIP_BUILD" != "1" ]]; then
  echo "[dual-vps] building binaries locally"
  go build -o hackme-node .
  go build -o coordinator ./cmd/coordinator
fi

echo "[dual-vps] syncing repository to NODE and COORD"
RSYNC_EXCLUDES=(--exclude '.git/' --exclude 'data/' --exclude 'reports/' --exclude 'node_modules/'
  --exclude 'dist/' --exclude 'backups/' --exclude 'logs/' --exclude '*.exe'
  --exclude '.env' --exclude '.env.*' --exclude '.cargo/' --exclude '.npm-global/' --exclude '.rustup/')
rsync -az --delete "${RSYNC_EXCLUDES[@]}" "$ROOT_DIR/" "$NODE_SSH:$NODE_DEPLOY_DIR/"
rsync -az --delete "${RSYNC_EXCLUDES[@]}" "$ROOT_DIR/" "$COORD_SSH:$COORD_DEPLOY_DIR/"

echo "[dual-vps] writing coordinator unit/env on COORD host"
ssh "$COORD_SSH" "bash -lc 'cat > $COORD_DEPLOY_DIR/.env.coord <<EOF
HACKME_COORDINATOR_ADDR=$COORD_ADDR
HACKME_COORDINATOR_ADMIN_TOKEN=$COORD_ADMIN_TOKEN
HACKME_COORDINATOR_DB=data/coordinator.db
HACKME_COORDINATOR_TARGET_SOURCE_URL=http://127.0.0.1:18080
HACKME_COORDINATOR_TARGET_REFRESH_SEC=3
HACKME_COORDINATOR_CLAIM_PER_MIN=600
HACKME_COORDINATOR_SUBMIT_PER_MIN=3000
EOF
cat > /etc/systemd/system/hackme-coordinator.service <<EOF
[Unit]
Description=HackMe Coordinator
After=network.target

[Service]
Type=simple
WorkingDirectory=$COORD_DEPLOY_DIR
EnvironmentFile=$COORD_DEPLOY_DIR/.env.coord
ExecStart=$COORD_DEPLOY_DIR/coordinator
Restart=always
RestartSec=2
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable hackme-coordinator.service
systemctl restart hackme-coordinator.service
'"

echo "[dual-vps] detecting COORD public IP"
COORD_IP="$(ssh "$COORD_SSH" "bash -lc 'hostname -I | tr -s \" \" \"\\n\" | head -n1'")"
if [[ -z "$COORD_IP" ]]; then
  echo "[dual-vps] failed to detect coordinator IP" >&2
  exit 1
fi
COORD_PUBLIC_URL="http://${COORD_IP}:18081"
echo "[dual-vps] coordinator public URL: $COORD_PUBLIC_URL"

echo "[dual-vps] writing node env on NODE host"
ssh "$NODE_SSH" "bash -lc 'cat > $NODE_DEPLOY_DIR/.env.vps <<EOF
HACKME_BIND_ADDR=$NODE_MAIN_ADDR
HACKME_ADMIN_TOKEN=$ADMIN_TOKEN
HACKME_P2P_TOKEN=$P2P_TOKEN
HACKME_POOL_COORDINATOR_URL=$COORD_PUBLIC_URL
HACKME_POOL_COORDINATOR_TOKEN=$COORD_ADMIN_TOKEN
HACKME_P2P_DISCOVERY=1
HACKME_P2P_ADVERTISE_URL=http://\$(hostname -I | tr -s \" \" \"\\n\" | head -n1):18080
HACKME_AUTO_START_MINING=1
EOF
cat > /etc/systemd/system/hackme-node.service <<EOF
[Unit]
Description=HackMe Node
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$NODE_DEPLOY_DIR
EnvironmentFile=$NODE_DEPLOY_DIR/.env.vps
ExecStart=$NODE_DEPLOY_DIR/hackme-node
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable hackme-node.service
systemctl restart hackme-node.service
'"

echo "[dual-vps] smoke checks"
NODE_IP="$(ssh "$NODE_SSH" "bash -lc 'hostname -I | tr -s \" \" \"\\n\" | head -n1'")"
NODE_BASE="http://${NODE_IP}:18080"

curl -fsS "$COORD_PUBLIC_URL/api/work/stats" | jq '{ok,workers_count,claim_per_min,submit_per_min}'
curl -fsS "$NODE_BASE/api/status" | jq '{tip_height,tip_hash,mining,node_address}'
curl -fsS "$NODE_BASE/api/global/metrics" | jq '{ok,global_source,chain:.chain.tip_height,work:.work.workers_count}'

echo
echo "[dual-vps] cutover complete"
echo "[dual-vps] node:  $NODE_BASE"
echo "[dual-vps] coord: $COORD_PUBLIC_URL"
echo "[dual-vps] next: tighten firewall so COORD :18081 is reachable only from trusted miner CIDRs"
