#!/usr/bin/env bash
# Install/update daily mirror snapshot cron on the local operator machine.
#
# Usage:
#   MIRROR_SSH=hackme-mirror bash scripts/ops/install_mirror_snapshot_cron.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MIRROR_SSH="${MIRROR_SSH:-}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
CRON_TIME="${CRON_TIME:-23 2 * * *}" # 02:23 daily
LOG_FILE="${LOG_FILE:-$ROOT/logs/mirror-snapshot-cron.log}"
LOG_TAG="[mirror-cron-install $(date -u +%Y-%m-%dT%H:%M:%SZ)]"

if [[ -z "$MIRROR_SSH" ]]; then
  echo "$LOG_TAG ERROR: set MIRROR_SSH (e.g. hackme-mirror)" >&2
  exit 1
fi

mkdir -p "$(dirname "$LOG_FILE")"
touch "$LOG_FILE"

cmd="cd $ROOT && NODE_SSH=$NODE_SSH MIRROR_SSH=$MIRROR_SSH bash scripts/ops/mirror_snapshot.sh >>$LOG_FILE 2>&1"
entry="$CRON_TIME $cmd"

tmp="$(mktemp)"
crontab -l 2>/dev/null | grep -v "scripts/ops/mirror_snapshot.sh" >"$tmp" || true
printf '%s\n' "$entry" >>"$tmp"
crontab "$tmp"
rm -f "$tmp"

echo "$LOG_TAG installed: $entry"
crontab -l | grep "mirror_snapshot.sh" || true
