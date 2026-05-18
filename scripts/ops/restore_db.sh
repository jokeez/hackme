#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DB_PATH="${DB_PATH:-$ROOT_DIR/data/hackme.db}"
BACKUP_TAR="${BACKUP_TAR:-}"

if [[ -z "$BACKUP_TAR" ]]; then
  echo "Usage: BACKUP_TAR=/path/to/hackme-db-*.tar.gz scripts/ops/restore_db.sh" >&2
  exit 1
fi
if [[ ! -f "$BACKUP_TAR" ]]; then
  echo "Backup tar not found: $BACKUP_TAR" >&2
  exit 1
fi

db_dir="$(dirname "$DB_PATH")"
db_file="$(basename "$DB_PATH")"
mkdir -p "$db_dir"

stamp="$(date -u +"%Y%m%dT%H%M%SZ")"

# Preserve current files if they exist.
if [[ -f "$DB_PATH" ]]; then mv -f "$DB_PATH" "$DB_PATH.pre-restore-$stamp"; fi
if [[ -f "$DB_PATH-wal" ]]; then mv -f "$DB_PATH-wal" "$DB_PATH-wal.pre-restore-$stamp"; fi
if [[ -f "$DB_PATH-shm" ]]; then mv -f "$DB_PATH-shm" "$DB_PATH-shm.pre-restore-$stamp"; fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
tar -C "$tmp_dir" -xzf "$BACKUP_TAR"

if [[ ! -f "$tmp_dir/$db_file" ]]; then
  echo "Invalid backup content: missing $db_file" >&2
  exit 1
fi

cp -f "$tmp_dir/$db_file" "$DB_PATH"
if [[ -f "$tmp_dir/$db_file-wal" ]]; then cp -f "$tmp_dir/$db_file-wal" "$DB_PATH-wal"; fi
if [[ -f "$tmp_dir/$db_file-shm" ]]; then cp -f "$tmp_dir/$db_file-shm" "$DB_PATH-shm"; fi

echo "Restore completed: $DB_PATH"
echo "Note: restart node process after restore."

