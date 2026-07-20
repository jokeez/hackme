#!/usr/bin/env bash
# One-shot mirror restore drill (no DNS cutover).
#
# Runs services on mirror, verifies local APIs, then optionally rolls back.
#
# Usage:
#   MIRROR_SSH=hackme-mirror bash scripts/ops/mirror_restore_drill.sh
#   MIRROR_SSH=hackme-mirror KEEP_RUNNING=1 bash scripts/ops/mirror_restore_drill.sh
set -euo pipefail

MIRROR_SSH="${MIRROR_SSH:-}"
KEEP_RUNNING="${KEEP_RUNNING:-0}"
LOG_TAG="[mirror-drill $(date -u +%Y-%m-%dT%H:%M:%SZ)]"

if [[ -z "$MIRROR_SSH" ]]; then
  echo "$LOG_TAG ERROR: set MIRROR_SSH (e.g. hackme-mirror)" >&2
  exit 1
fi

echo "$LOG_TAG start on $MIRROR_SSH"

ssh -o BatchMode=yes "$MIRROR_SSH" "sudo systemctl start hackme-node"
sleep 4
ssh -o BatchMode=yes "$MIRROR_SSH" \
  "curl -fsS --max-time 10 http://127.0.0.1:18080/api/status \
    | jq '{commit,chain_height,network_mode_active}'"

ssh -o BatchMode=yes "$MIRROR_SSH" "sudo systemctl start hackme-coordinator"
sleep 3
ssh -o BatchMode=yes "$MIRROR_SSH" \
  "curl -fsS --max-time 10 http://127.0.0.1:18081/health | jq '{ok,service}'"

if ssh -o BatchMode=yes "$MIRROR_SSH" "command -v nginx >/dev/null 2>&1"; then
  ssh -o BatchMode=yes "$MIRROR_SSH" "sudo systemctl start nginx 2>/dev/null || true"
  ssh -o BatchMode=yes "$MIRROR_SSH" \
    "curl -fsS --max-time 8 http://127.0.0.1/ >/dev/null && echo '$LOG_TAG nginx local HTTP OK'"
else
  echo "$LOG_TAG WARN nginx not installed on mirror (TLS cutover not ready yet)"
fi

echo "$LOG_TAG PASS: node+coordinator drill completed"

if [[ "$KEEP_RUNNING" != "1" ]]; then
  ssh -o BatchMode=yes "$MIRROR_SSH" \
    "sudo systemctl stop hackme-coordinator hackme-node nginx 2>/dev/null || true"
  echo "$LOG_TAG rollback: services stopped"
else
  echo "$LOG_TAG KEEP_RUNNING=1: services left running"
fi
