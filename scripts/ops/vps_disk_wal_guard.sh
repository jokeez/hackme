#!/usr/bin/env bash
# VPS guard: disk space + SQLite WAL checkpoint (prevents 34GB WAL blowups).
set -euo pipefail

DEPLOY="${DEPLOY:-/opt/hackme}"
DB="${HACKME_DB:-${DEPLOY}/data/hackme.db}"
WARN_PCT="${WARN_PCT:-85}"
CRIT_PCT="${CRIT_PCT:-92}"

log() { echo "[disk-wal-guard $(date -u +%H:%M:%S)] $*"; }

df_line="$(df -P / | tail -1)"
used_pct="$(awk '{print $5}' <<<"$df_line" | tr -d '%')"
avail_kb="$(awk '{print $4}' <<<"$df_line")"
log "disk used=${used_pct}% avail_kb=${avail_kb}"

NODE_STOPPED=0
if [[ "$used_pct" -ge "$CRIT_PCT" ]]; then
  log "CRIT ${used_pct}% — vacuum journal"
  journalctl --vacuum-size=200M 2>/dev/null || sudo journalctl --vacuum-size=200M 2>/dev/null || true
  # Stop node before WAL truncate when disk is full (checkpoint needs exclusive lock).
  if systemctl is-active hackme-node >/dev/null 2>&1; then
    log "stopping hackme-node for WAL checkpoint"
    systemctl stop hackme-node 2>/dev/null || sudo systemctl stop hackme-node 2>/dev/null || true
    sleep 2
    NODE_STOPPED=1
  fi
fi

if [[ -f "$DB" ]] && command -v sqlite3 >/dev/null 2>&1; then
  wal="$(ls -l "${DB}-wal" 2>/dev/null | awk '{print $5}' || echo 0)"
  if [[ "${wal:-0}" -gt 536870912 ]] || [[ "$used_pct" -ge "$WARN_PCT" ]]; then
    log "WAL bytes=${wal:-0} — checkpoint TRUNCATE"
    if systemctl is-active hackme-node >/dev/null 2>&1; then
      sqlite3 "$DB" "PRAGMA wal_checkpoint(TRUNCATE);" 2>/dev/null || \
        sudo -u hackme sqlite3 "$DB" "PRAGMA wal_checkpoint(TRUNCATE);" 2>/dev/null || true
    else
      sqlite3 "$DB" "PRAGMA wal_checkpoint(TRUNCATE);" 2>/dev/null || true
    fi
    wal_after="$(ls -l "${DB}-wal" 2>/dev/null | awk '{print $5}' || echo 0)"
    log "WAL after=${wal_after:-0}"
  fi
fi

df -P / | tail -1
if [[ "${NODE_STOPPED:-0}" == "1" ]]; then
  log "restarting hackme-node after WAL checkpoint"
  systemctl start hackme-node 2>/dev/null || sudo systemctl start hackme-node 2>/dev/null || true
fi
log "OK"
