#!/usr/bin/env bash
# VPS disk cleanup: nginx logs, SQLite WAL, dev dist junk, old backups.
# Run on VPS as root:
#   ssh hackme-vps 'sudo bash -s' < scripts/ops/vps_disk_cleanup.sh
set -euo pipefail

DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
REL="${RELEASE_VER:-0.1.0-rc11m}"
log() { echo "[vps-disk] $*"; }

log "before: $(df -h / | tail -1 | awk '{print $3" used, "$4" free ("$5")"}')"

log "nginx access/error — truncate bloated logs + reopen"
for f in /var/log/nginx/access.log /var/log/nginx/error.log; do
  if [[ -f "$f" ]]; then
    sz=$(du -m "$f" | awk '{print $1}')
    if [[ "$sz" -gt 200 ]]; then
      : >"$f"
      log "truncated ${f} (was ${sz}MB)"
    fi
  fi
done
nginx -s reopen 2>/dev/null || systemctl reload nginx

log "drop old nginx .gz (>14 days)"
find /var/log/nginx -maxdepth 1 -name '*.gz' -mtime +14 -delete 2>/dev/null || true

log "journal vacuum (keep 400M)"
journalctl --vacuum-size=400M 2>/dev/null || true

log "remove dev/test dist junk (dist root only — never release_${REL})"
rm -rf "${DEPLOY}/dist/vast-gpu-matrix-"* 2>/dev/null || true
rm -f "${DEPLOY}/dist/vast_pack.part."* "${DEPLOY}/dist/upload_"*.bin "${DEPLOY}/dist/tiny_test.txt" 2>/dev/null || true
rm -f "${DEPLOY}/dist/hackme-phasing-"* 2>/dev/null || true
rm -rf "${DEPLOY}/dist/windows" 2>/dev/null || true
# Loose fuzzing copies in dist/ root only (release bundle lives under dist/release_*)
find "${DEPLOY}/dist" -maxdepth 1 -type f -name 'hackme-fuzzing-*' -delete 2>/dev/null || true

log "prune DB backups (keep newest 2)"
if [[ -d "${DEPLOY}/backups" ]]; then
  mapfile -t old < <(ls -1t "${DEPLOY}/backups"/hackme-db-*.tar.gz 2>/dev/null | tail -n +3 || true)
  for f in "${old[@]}"; do rm -f "$f" && log "removed backup $f"; done
fi

log "rotate huge app logs (>20M)"
for f in "${DEPLOY}/logs/worker_participant.log" "${DEPLOY}/logs/news-bot.log"; do
  if [[ -f "$f" ]] && [[ $(stat -c%s "$f" 2>/dev/null || echo 0) -gt 20971520 ]]; then
    mv "$f" "${f}.$(date -u +%Y%m%dT%H%M%SZ)"
    : >"$f"
    log "rotated $f"
  fi
done

log "SQLite WAL checkpoint"
if [[ -f "${DEPLOY}/data/hackme.db" ]]; then
  command -v sqlite3 >/dev/null || { apt-get update -qq && apt-get install -y -qq sqlite3 >/dev/null; }
  systemctl stop hackme-node hackme-coordinator 2>/dev/null || true
  sleep 2
  sqlite3 "${DEPLOY}/data/hackme.db" 'PRAGMA wal_checkpoint(TRUNCATE);'
  sqlite3 "${DEPLOY}/data/coordinator.db" 'PRAGMA wal_checkpoint(TRUNCATE);' 2>/dev/null || true
  chown -R hackme:hackme "${DEPLOY}/data" 2>/dev/null || true
  systemctl start hackme-node hackme-coordinator 2>/dev/null || true
  sleep 8
  wal=$(du -m "${DEPLOY}/data/hackme.db-wal" 2>/dev/null | awk '{print $1}' || echo 0)
  log "hackme.db-wal after checkpoint: ${wal}MB"
fi

log "after: $(df -h / | tail -1 | awk '{print $3" used, "$4" free ("$5")"}')"
log "services: $(systemctl is-active nginx hackme-node hackme-coordinator 2>/dev/null | paste -sd, -)"
