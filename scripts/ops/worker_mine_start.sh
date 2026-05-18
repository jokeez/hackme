#!/usr/bin/env bash
set -euo pipefail

# Start PC worker mining against remote coordinator.
# Keeps local chain mining OFF; launches worker_loop in background.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[worker-mine-start] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd curl
require_cmd jq
require_cmd nohup

COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
WORKER_ID="${WORKER_ID:-pc-kapa-01}"
BATCH_SIZE="${BATCH_SIZE:-2000000}"
HASHRATE_GHS="${HASHRATE_GHS:-42.5}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
WORKER_LOG="${WORKER_LOG:-$ROOT_DIR/logs/worker_participant.log}"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[worker-mine-start] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 1
fi

mkdir -p "$(dirname "$WORKER_LOG")"

echo "[worker-mine-start] stop local node mining (worker mode safety)"
curl -fsS -X POST "${LOCAL_BASE}/api/mining/stop" \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null 2>&1 || true

echo "[worker-mine-start] restart worker loop -> ${COORD_URL}"
pkill -f "scripts/ops/worker_loop.sh" >/dev/null 2>&1 || true

nohup env \
  COORD_URL="${COORD_URL}" \
  COORD_ADMIN_TOKEN="${ADMIN_TOKEN}" \
  WORKER_ID="${WORKER_ID}" \
  BATCH_SIZE="${BATCH_SIZE}" \
  HASHRATE_GHS="${HASHRATE_GHS}" \
  bash "$ROOT_DIR/scripts/ops/worker_loop.sh" >"${WORKER_LOG}" 2>&1 &

sleep 1
tail -n 8 "${WORKER_LOG}" || true
echo "[worker-mine-start] worker log: ${WORKER_LOG}"
