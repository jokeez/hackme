#!/usr/bin/env bash
set -euo pipefail

# Participant-side helper for stable worker mode:
# - align local follower with VPS canon
# - keep local chain mining OFF
# - start worker loop against VPS coordinator

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[worker-participant-up] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd curl
require_cmd jq
require_cmd nohup

VPS_SSH="${VPS_SSH:-}"
VPS_BASE="${VPS_BASE:-https://hackme.tech}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
COORD_TOKEN="${COORD_TOKEN:-${COORD_ADMIN_TOKEN:-${HACKME_POOL_COORDINATOR_TOKEN:-${ADMIN_TOKEN:-}}}}"
ADVERTISE_URL="${ADVERTISE_URL:-}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}"
WORKER_ID="${WORKER_ID:-worker-$(hostname)-01}"
BATCH_SIZE="${BATCH_SIZE:-2000000}"
HASHRATE_GHS="${HASHRATE_GHS:-0.01}"
WORKER_LOG="${WORKER_LOG:-$ROOT_DIR/logs/worker_participant.log}"

if [[ -z "$VPS_SSH" ]]; then
  echo "[worker-participant-up] VPS_SSH is required (e.g. root@132.243.112.100)" >&2
  exit 1
fi
if [[ -z "$ADVERTISE_URL" ]]; then
  echo "[worker-participant-up] ADVERTISE_URL is required (e.g. http://192.168.1.113:8080)" >&2
  exit 1
fi
if [[ -z "$ADMIN_TOKEN" || -z "$P2P_TOKEN" ]]; then
  echo "[worker-participant-up] ADMIN_TOKEN and P2P_TOKEN are required" >&2
  exit 1
fi

echo "[worker-participant-up] align follower with VPS canon (mining OFF)"
VPS_SSH="$VPS_SSH" \
VPS_BASE="$VPS_BASE" \
ADVERTISE_URL="$ADVERTISE_URL" \
ADMIN_TOKEN="$ADMIN_TOKEN" \
P2P_TOKEN="$P2P_TOKEN" \
RUN_AUTOPILOT=0 \
AUTO_START_MINING_WHEN_SYNCED=0 \
bash "$ROOT_DIR/scripts/ops/prefinal_public_sync.sh"

echo "[worker-participant-up] ensuring local mining stays OFF"
curl -fsS -X POST "http://127.0.0.1:8080/api/mining/stop" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null 2>&1 || true

echo "[worker-participant-up] starting worker loop -> ${COORD_URL}"
pkill -f "scripts/ops/worker_loop.sh" >/dev/null 2>&1 || true
mkdir -p "$(dirname "$WORKER_LOG")"
nohup env \
  COORD_URL="${COORD_URL}" \
  COORD_ADMIN_TOKEN="${COORD_TOKEN}" \
  WORKER_ID="${WORKER_ID}" \
  BATCH_SIZE="${BATCH_SIZE}" \
  HASHRATE_GHS="${HASHRATE_GHS}" \
  bash "$ROOT_DIR/scripts/ops/worker_loop.sh" >"${WORKER_LOG}" 2>&1 &

sleep 2
echo "[worker-participant-up] local status:"
curl -sS "http://127.0.0.1:8080/api/status" | jq '{tip_height,tip_hash,node_address,mining}'
echo "[worker-participant-up] coordinator status:"
curl -sS "${COORD_URL}/api/work/stats" | jq '{ok,summary:(.summary // {}),last:(.last // {})}' || true
echo "[worker-participant-up] worker log: ${WORKER_LOG}"
echo "[worker-participant-up] done"

