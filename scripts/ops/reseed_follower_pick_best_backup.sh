#!/usr/bin/env bash
set -euo pipefail
#
# Pick hackme-db-*.tar.gz in BACKUP_DIR with the highest blocks.MAX(block_index) and restore into DB_PATH.
# Requires hackme-node (and anything locking SQLite) stopped — otherwise restore_db mv may fail or corrupt.
#
# Usage:
#   bash scripts/ops/reseed_follower_pick_best_backup.sh
#   BACKUP_DIR=/path/to/backups DB_PATH=/path/to/data/hackme.db bash scripts/ops/reseed_follower_pick_best_backup.sh
#
# Note: if leader tip is ahead of every tarball, you still need a fresh VPS snapshot (follower_bootstrap_from_vps.sh).

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

BACKUP_DIR="${BACKUP_DIR:-$ROOT_DIR/backups}"
DB_PATH="${DB_PATH:-$ROOT_DIR/data/hackme.db}"

require_cmd() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "[reseed-best-backup] missing command: $1" >&2
		exit 1
	}
}
require_cmd sqlite3
require_cmd tar

if pgrep -x hackme-node >/dev/null 2>&1 || pgrep -f '/hackme-node' >/dev/null 2>&1; then
	echo "[reseed-best-backup] ERROR: hackme-node is running — stop it before restoring SQLite." >&2
	exit 1
fi

shopt -s nullglob
candidates=("$BACKUP_DIR"/hackme-db-*.tar.gz)
if [[ ${#candidates[@]} -eq 0 ]]; then
	echo "[reseed-best-backup] no hackme-db-*.tar.gz in $BACKUP_DIR" >&2
	exit 1
fi

best_tar=""
best_tip=-1
for t in "${candidates[@]}"; do
	tmp="$(mktemp -d)"
	if ! tar -xzf "$t" -C "$tmp" >/dev/null 2>&1; then
		rm -rf "$tmp"
		continue
	fi
	dbf="$tmp/hackme.db"
	if [[ ! -f "$dbf" ]]; then
		rm -rf "$tmp"
		continue
	fi
	tip="$(sqlite3 "$dbf" "SELECT COALESCE(MAX(block_index),0) FROM blocks;" 2>/dev/null || echo 0)"
	rm -rf "$tmp"
	if [[ "$tip" =~ ^[0-9]+$ ]] && [[ "$tip" -gt "$best_tip" ]]; then
		best_tip=$tip
		best_tar=$t
	fi
done

if [[ -z "$best_tar" ]]; then
	echo "[reseed-best-backup] could not read any tarball" >&2
	exit 1
fi

echo "[reseed-best-backup] restoring tip_height=$best_tip from $(basename "$best_tar")"
export BACKUP_TAR="$best_tar"
export DB_PATH
bash "$ROOT_DIR/scripts/ops/restore_db.sh"
