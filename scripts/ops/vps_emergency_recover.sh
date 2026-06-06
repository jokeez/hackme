#!/usr/bin/env bash
# Emergency VPS recovery after Cloudflare 524 / hung origin.
# Run ON the VPS as root (SSH or hosting console):
#   bash /opt/hackme/scripts/ops/vps_emergency_recover.sh
# Or pipe from laptop when SSH works:
#   HACKME_DEPLOY_SSH_IDENTITY=~/.ssh/cursor_vps ssh hackme-vps 'sudo bash -s' < scripts/ops/vps_emergency_recover.sh
set -euo pipefail

DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
log() { echo "[vps-recover] $*"; }

log "=== host snapshot ==="
uptime || true
free -h || true
df -h / /opt "${DEPLOY}" 2>/dev/null || df -h /
log "drop stale nginx/node if hung (SIGTERM first)"
systemctl stop hackme-node hackme-coordinator 2>/dev/null || true
sleep 2
pkill -f 'go run \.' 2>/dev/null || true
pkill -f '/opt/hackme/hackme-node' 2>/dev/null || true
sleep 1

if [[ -f "${DEPLOY}/scripts/ops/systemd/hackme-node.service" ]]; then
  log "install production hackme-node.service (binary, not go run)"
  cp -a "${DEPLOY}/scripts/ops/systemd/hackme-node.service" /etc/systemd/system/hackme-node.service
fi

if [[ ! -x "${DEPLOY}/hackme-node" ]]; then
  log "WARN: ${DEPLOY}/hackme-node missing — build on laptop and deploy_hackme_node.sh"
else
  chmod 755 "${DEPLOY}/hackme-node"
  chown hackme:hackme "${DEPLOY}/hackme-node" 2>/dev/null || true
fi

if [[ -f "${DEPLOY}/scripts/ops/vps_patch_nginx_connections.sh" ]]; then
  bash "${DEPLOY}/scripts/ops/vps_patch_nginx_connections.sh" || true
fi

log "nginx test + restart"
nginx -t
systemctl restart nginx
systemctl is-active nginx

log "start node + coordinator"
systemctl daemon-reload
systemctl enable hackme-node 2>/dev/null || true
systemctl restart hackme-node
systemctl enable hackme-coordinator 2>/dev/null || true
if systemctl list-unit-files | grep -q hackme-coordinator; then
  systemctl restart hackme-coordinator 2>/dev/null || true
fi
sleep 15

log "loopback smoke"
curl -fsS --max-time 5 -o /dev/null -w 'static_index:%{http_code}\n' http://127.0.0.1/index.html || echo "static FAIL"
curl -fsS --max-time 8 http://127.0.0.1:18080/api/status?lite=1 | head -c 200 || echo "node FAIL"
curl -fsS --max-time 5 http://127.0.0.1:18081/health 2>/dev/null | head -c 80 || echo "coord optional"

log "done — check https://hackme.tech/ and https://hackme.tech/healthz.html"
