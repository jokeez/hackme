#!/usr/bin/env bash
# Fix common VPS prod issues: disk pressure, corrupt release launchers, SQLite WAL bloat.
# Run on VPS as root after SSH is up:
#   ssh hackme-vps 'sudo bash -s' < scripts/ops/vps_prod_fix.sh
set -euo pipefail

DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
REL="${RELEASE_VER:-0.1.0-rc11l}"
REL_DIR="${DEPLOY}/dist/release_${REL}"
log() { echo "[vps-prod-fix] $*"; }

log "disk before"
df -h / | tail -1

log "fix corrupt HackMe-Install.cmd (was zip blob on some deploys)"
if [[ -f "${DEPLOY}/scripts/release/windows/HackMe-Install.cmd" ]]; then
  install -m 644 "${DEPLOY}/scripts/release/windows/HackMe-Install.cmd" "${REL_DIR}/HackMe-Install.cmd"
elif [[ -f /tmp/HackMe-Install.cmd ]]; then
  install -m 644 /tmp/HackMe-Install.cmd "${REL_DIR}/HackMe-Install.cmd"
fi
if [[ -f "${REL_DIR}/HackMe-Install.cmd" ]]; then
  sz=$(wc -c <"${REL_DIR}/HackMe-Install.cmd")
  if [[ "$sz" -gt 4096 ]]; then
    log "WARN: HackMe-Install.cmd still large (${sz} bytes) — replace manually"
  else
    log "OK HackMe-Install.cmd size=${sz}"
  fi
fi

log "prune old dist releases (keep current ${REL})"
if [[ -d "${DEPLOY}/dist" ]]; then
  find "${DEPLOY}/dist" -maxdepth 1 -type d -name 'release_*' ! -name "release_${REL}" -print -exec rm -rf {} + 2>/dev/null || true
fi

log "SQLite WAL checkpoint (reduces hackme.db-wal bloat)"
if command -v sqlite3 >/dev/null && [[ -f "${DEPLOY}/data/hackme.db" ]]; then
  systemctl stop hackme-node hackme-coordinator 2>/dev/null || true
  sleep 2
  sqlite3 "${DEPLOY}/data/hackme.db" 'PRAGMA wal_checkpoint(TRUNCATE);' || true
  sqlite3 "${DEPLOY}/data/coordinator.db" 'PRAGMA wal_checkpoint(TRUNCATE);' 2>/dev/null || true
  systemctl start hackme-node hackme-coordinator 2>/dev/null || true
  sleep 3
fi

log "ensure coordinator running"
systemctl enable hackme-coordinator 2>/dev/null || true
systemctl restart hackme-coordinator 2>/dev/null || true
sleep 2

log "install production systemd unit if present"
if [[ -f "${DEPLOY}/scripts/ops/systemd/hackme-node.service" ]]; then
  cp -a "${DEPLOY}/scripts/ops/systemd/hackme-node.service" /etc/systemd/system/hackme-node.service
  systemctl daemon-reload
  systemctl restart hackme-node
fi

log "nginx reload"
nginx -t
systemctl reload nginx

log "disk after"
df -h / | tail -1
log "loopback smoke"
curl -fsS --max-time 8 -o /dev/null -w 'index:%{http_code}\n' http://127.0.0.1/index.html || true
curl -fsS --max-time 8 -o /dev/null -w 'setup:%{http_code}\n' "http://127.0.0.1/dist/release_${REL}/HackMe-Setup-${REL}.exe" || true
