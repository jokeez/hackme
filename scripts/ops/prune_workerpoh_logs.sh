#!/usr/bin/env bash
# Retain recent pool worker logs; drop older workerpoh-*.log files (hub disk hygiene).
set -euo pipefail

LOG_DIR="${1:-/opt/hackme/logs}"
KEEP_DAYS="${KEEP_DAYS:-14}"
KEEP_COUNT="${KEEP_COUNT:-500}"

if [[ ! -d "$LOG_DIR" ]]; then
  echo "[prune-workerpoh] skip: missing $LOG_DIR" >&2
  exit 0
fi

mapfile -t files < <(find "$LOG_DIR" -maxdepth 1 -type f -name 'workerpoh-*.log' -printf '%T@ %p\n' 2>/dev/null | sort -rn | cut -d' ' -f2-)
total="${#files[@]}"
if [[ "$total" -eq 0 ]]; then
  echo "[prune-workerpoh] no workerpoh logs under $LOG_DIR"
  exit 0
fi

cutoff_epoch="$(date -d "-${KEEP_DAYS} days" +%s 2>/dev/null || date -v-"${KEEP_DAYS}"d +%s)"
removed=0
for ((i = 0; i < total; i++)); do
  f="${files[$i]}"
  [[ -f "$f" ]] || continue
  if (( i < KEEP_COUNT )); then
    continue
  fi
  mtime="$(stat -c %Y "$f" 2>/dev/null || stat -f %m "$f")"
  if [[ "$mtime" -ge "$cutoff_epoch" ]]; then
    continue
  fi
  rm -f "$f" && removed=$((removed + 1))
done
echo "[prune-workerpoh] dir=$LOG_DIR total=$total removed=$removed keep_count=$KEEP_COUNT keep_days=$KEEP_DAYS"
