#!/usr/bin/env bash
# Poll VPS every 5 minutes; when SSH is back, run full recover + deploy.
# Leave running while away:
#   HACKME_DEPLOY_SSH_IDENTITY=~/.ssh/cursor_vps bash scripts/ops/vps_watch_and_recover.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

NODE_SSH="${NODE_SSH:-root@132.243.112.100}"
IDENT="${HACKME_DEPLOY_SSH_IDENTITY:-$HOME/.ssh/cursor_vps}"
INTERVAL_SEC="${INTERVAL_SEC:-300}"
MAX_HOURS="${MAX_HOURS:-48}"
LOG_DIR="${LOG_DIR:-$ROOT/logs}"
mkdir -p "$LOG_DIR"
LOG="$LOG_DIR/vps-watch-$(date -u +%Y%m%dT%H%M%SZ).log"

log() { echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] [vps-watch] $*" | tee -a "$LOG"; }

_ssh_ok() {
  ssh -i "$IDENT" -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=25 \
    -o StrictHostKeyChecking=accept-new "$NODE_SSH" 'echo up' >/dev/null 2>&1
}

_site_ok() {
  local code
  code="$(curl -fsS --max-time 20 -o /dev/null -w '%{http_code}' 'https://hackme.tech/' 2>/dev/null || echo 000)"
  [[ "$code" == "200" ]]
}

deadline=$(( $(date +%s) + MAX_HOURS * 3600 ))
attempt=0
log "start interval=${INTERVAL_SEC}s max_hours=${MAX_HOURS} ssh=${NODE_SSH} log=$LOG"

while (( $(date +%s) < deadline )); do
  attempt=$((attempt + 1))
  if _site_ok; then
    log "site already OK (https://hackme.tech/) — done"
    exit 0
  fi
  if _ssh_ok; then
    log "SSH up (attempt $attempt) — running recover+deploy"
    if HACKME_DEPLOY_SSH_IDENTITY="$IDENT" NODE_SSH="$NODE_SSH" WAIT_SEC=120 INTERVAL=10 \
      bash "$ROOT/scripts/ops/vps_recover_and_deploy.sh" 2>&1 | tee -a "$LOG"; then
      log "recover OK — https://hackme.tech/ is live"
      exit 0
    fi
    log "recover failed — will retry after ${INTERVAL_SEC}s"
  else
    log "attempt $attempt: SSH down, site down — sleep ${INTERVAL_SEC}s"
  fi
  sleep "$INTERVAL_SEC"
done

log "FATAL: gave up after ${MAX_HOURS}h — check VPS panel / reboot manually"
exit 1
