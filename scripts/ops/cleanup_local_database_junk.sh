#!/usr/bin/env bash
# Remove redundant local SQLite trees and restore snapshots under the repo workspace.
# Does NOT touch public-chain treasury (genesis mint is consensus-locked to DevFeeAddress).
#
# Usage:
#   DRY_RUN=1 bash scripts/ops/cleanup_local_database_junk.sh   # print only (default)
#   CONFIRM=1 bash scripts/ops/cleanup_local_database_junk.sh   # delete junk
# Optional:
#   PURGE_DESKTOP_DATA=1   — also rm logs/desktop/data/hackme.db* (fresh desktop DB next start)
#   PURGE_MANUAL_BACKUP=1 — also rm backups/manual_fix_*
#   PURGE_DATA_TAR=1      — also rm backups/*.tar.gz (large; off by default)

set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

DRY_RUN="${DRY_RUN:-1}"
CONFIRM="${CONFIRM:-0}"
PURGE_DESKTOP_DATA="${PURGE_DESKTOP_DATA:-0}"
PURGE_MANUAL_BACKUP="${PURGE_MANUAL_BACKUP:-0}"
PURGE_DATA_TAR="${PURGE_DATA_TAR:-0}"

if [[ "$CONFIRM" == "1" ]]; then
  DRY_RUN=0
fi

rm_path() {
  local p="$1"
  if [[ ! -e "$p" ]]; then
    return 0
  fi
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "[dry-run] would remove: $p"
  else
    rm -rf "$p"
    echo "[removed] $p"
  fi
}

echo "[cleanup] ROOT=$ROOT_DIR dry_run=$DRY_RUN confirm=$CONFIRM"

# --- Always safe: old restore snapshots next to main DB ---
shopt -s nullglob
for f in "$ROOT_DIR"/data/hackme.db.pre-restore-*; do
  rm_path "$f"
done
shopt -u nullglob

# --- One-off backup dirs at repo root ---
rm_path "$ROOT_DIR/data_backup_20260426_203131"
rm_path "$ROOT_DIR/data_tmp_20260426_203237"
rm_path "$ROOT_DIR/data_tmp_20260426_203243"

# --- Isolated solo fork (not public pool state) ---
rm_path "$ROOT_DIR/data/local-treasury-mine"

# --- Ephemeral local mining harness trees ---
shopt -s nullglob
for d in "$ROOT_DIR"/logs/local_mining_*; do
  if [[ -d "$d" ]]; then
    rm_path "$d"
  fi
done
shopt -u nullglob

# --- Alternate coordinator DBs (keep data/coordinator.db for local pool) ---
for pat in coordinator-ci coordinator_local; do
  rm_path "$ROOT_DIR/data/${pat}.db"
  rm_path "$ROOT_DIR/data/${pat}.db-shm"
  rm_path "$ROOT_DIR/data/${pat}.db-wal"
done

if [[ "$PURGE_DESKTOP_DATA" == "1" ]]; then
  rm_path "$ROOT_DIR/logs/desktop/data/hackme.db"
  rm_path "$ROOT_DIR/logs/desktop/data/hackme.db-shm"
  rm_path "$ROOT_DIR/logs/desktop/data/hackme.db-wal"
fi

if [[ "$PURGE_MANUAL_BACKUP" == "1" ]]; then
  rm_path "$ROOT_DIR/backups/manual_fix_20260426_202750"
fi

if [[ "$PURGE_DATA_TAR" == "1" ]]; then
  shopt -s nullglob
  for f in "$ROOT_DIR"/backups/*.tar.gz; do
    rm_path "$f"
  done
  shopt -u nullglob
fi

if [[ "$DRY_RUN" == "1" ]]; then
  echo ""
  echo "To apply deletions: CONFIRM=1 bash scripts/ops/cleanup_local_database_junk.sh"
  echo "Optional: PURGE_DESKTOP_DATA=1 PURGE_MANUAL_BACKUP=1 PURGE_DATA_TAR=1"
fi
