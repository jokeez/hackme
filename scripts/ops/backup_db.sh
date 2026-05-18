#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DB_PATH="${DB_PATH:-$ROOT_DIR/data/hackme.db}"
BACKUP_DIR="${BACKUP_DIR:-$ROOT_DIR/backups}"
STAMP="$(date -u +"%Y%m%dT%H%M%SZ")"
OUT="$BACKUP_DIR/hackme-db-$STAMP.tar.gz"

mkdir -p "$BACKUP_DIR"

db_dir="$(dirname "$DB_PATH")"
db_file="$(basename "$DB_PATH")"
wal_file="$DB_PATH-wal"
shm_file="$DB_PATH-shm"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

if [[ ! -f "$DB_PATH" ]]; then
  echo "DB not found: $DB_PATH" >&2
  exit 1
fi

cp -f "$DB_PATH" "$tmp_dir/$db_file"
if [[ -f "$wal_file" ]]; then cp -f "$wal_file" "$tmp_dir/$db_file-wal"; fi
if [[ -f "$shm_file" ]]; then cp -f "$shm_file" "$tmp_dir/$db_file-shm"; fi

cat >"$tmp_dir/METADATA.txt" <<EOF
created_at_utc=$STAMP
db_path=$DB_PATH
host=$(hostname)
EOF

tar -C "$tmp_dir" -czf "$OUT" .
echo "Backup created: $OUT"

