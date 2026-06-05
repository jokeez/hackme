#!/usr/bin/env bash
# Wait for VPS SSH, run emergency recover, deploy binary node + static site.
# Usage (laptop):
#   HACKME_DEPLOY_SSH_IDENTITY=~/.ssh/cursor_vps bash scripts/ops/vps_recover_and_deploy.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/ops/_deploy_ssh_retry.sh
source "$ROOT/scripts/ops/_deploy_ssh_retry.sh"

NODE_SSH="${NODE_SSH:-hackme-vps}"
IDENT="${HACKME_DEPLOY_SSH_IDENTITY:-}"
WAIT_SEC="${WAIT_SEC:-900}"
INTERVAL="${INTERVAL:-15}"

_ssh() {
  if [[ -n "$IDENT" && -f "$IDENT" ]]; then
    ssh -i "$IDENT" -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=20 -o StrictHostKeyChecking=accept-new "$@"
  else
    ssh -o BatchMode=yes -o ConnectTimeout=20 "$@"
  fi
}

log() { echo "[recover-deploy] $*"; }

deadline=$(( $(date +%s) + WAIT_SEC ))
log "waiting for SSH to $NODE_SSH (max ${WAIT_SEC}s)..."
while (( $(date +%s) < deadline )); do
  if _ssh "$NODE_SSH" 'echo up' >/dev/null 2>&1; then
    log "SSH OK"
    break
  fi
  sleep "$INTERVAL"
done
_ssh "$NODE_SSH" 'echo up' >/dev/null 2>&1 || {
  log "FATAL: SSH still down — reboot VPS from hosting panel, then re-run this script"
  exit 1
}

log "rsync recovery script + systemd unit"
if [[ -n "$IDENT" && -f "$IDENT" ]]; then
  rsync -e "ssh -i $IDENT -o IdentitiesOnly=yes -o BatchMode=yes" -az \
    "$ROOT/scripts/ops/vps_emergency_recover.sh" \
    "$ROOT/scripts/ops/systemd/hackme-node.service" \
    "$NODE_SSH:/tmp/"
else
  scp "$ROOT/scripts/ops/vps_emergency_recover.sh" "$ROOT/scripts/ops/systemd/hackme-node.service" "$NODE_SSH:/tmp/"
fi

log "deploy binary node (fixes go run OOM)"
HACKME_DEPLOY_SSH_IDENTITY="$IDENT" NODE_SSH="$NODE_SSH" bash "$ROOT/scripts/ops/deploy_hackme_node.sh"

log "emergency recover on VPS"
_ssh "$NODE_SSH" "sudo cp /tmp/hackme-node.service /etc/systemd/system/ && sudo bash /tmp/vps_emergency_recover.sh"

log "deploy static site + healthz + nginx vhost"
HACKME_DEPLOY_SSH_IDENTITY="$IDENT" NODE_SSH="$NODE_SSH" SKIP_DIST=1 SYNC_NGINX_SITE_CONF=1 bash "$ROOT/scripts/ops/deploy_hackme_site.sh"

log "public smoke (pages + API)"
for path in / /index.html /healthz.html /downloads.html /contacts.html; do
  code="$(curl -fsS --max-time 25 -o /dev/null -w '%{http_code}' "https://hackme.tech${path}" 2>/dev/null || echo 000)"
  log "  ${path} -> HTTP ${code}"
  [[ "$code" == "200" ]] || { log "FATAL: ${path} not 200"; exit 1; }
done
api_code="$(curl -fsS --max-time 20 -o /dev/null -w '%{http_code}' 'https://hackme.tech/pool/api/status?lite=1' 2>/dev/null || echo 000)"
log "  /pool/api/status -> HTTP ${api_code}"
[[ "$api_code" == "200" ]] || log "WARN: node API not 200 yet (may still be starting)"
coord_code="$(curl -fsS --max-time 15 -o /dev/null -w '%{http_code}' 'https://hackme.tech/pool/coordinator/health' 2>/dev/null || echo 000)"
log "  /pool/coordinator/health -> HTTP ${coord_code}"
[[ "$coord_code" == "200" ]] || log "WARN: coordinator health not 200"

code="$(curl -fsS --max-time 25 -o /dev/null -w '%{http_code}' 'https://hackme.tech/' || echo 000)"
log "public smoke: https://hackme.tech/ -> HTTP $code"
[[ "$code" == "200" ]] || exit 1
log "OK — hackme.tech restored"
