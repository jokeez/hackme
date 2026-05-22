#!/usr/bin/env bash
set -euo pipefail

# Red-button recovery for worker mode:
# 1) freeze mining where needed
# 2) re-align local follower with VPS canon
# 3) restart worker loop
# 4) run health check

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[worker-recover] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd curl
require_cmd jq

VPS_SSH="${VPS_SSH:-}"
VPS_BASE="${VPS_BASE:-http://hackme-vps:18080}"
COORD_URL="${COORD_URL:-http://hackme-vps:18081}"
ADVERTISE_URL="${ADVERTISE_URL:-}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}"
WORKER_ID="${WORKER_ID:-worker-$(hostname)-01}"
BATCH_SIZE="${BATCH_SIZE:-2000000}"
HASHRATE_GHS="${HASHRATE_GHS:-42.5}"

if [[ -z "$VPS_SSH" || -z "$ADVERTISE_URL" || -z "$ADMIN_TOKEN" || -z "$P2P_TOKEN" ]]; then
  echo "[worker-recover] required: VPS_SSH, ADVERTISE_URL, ADMIN_TOKEN, P2P_TOKEN" >&2
  exit 1
fi

echo "[worker-recover] step 1/4: re-align follower with VPS canon"
VPS_SSH="$VPS_SSH" \
VPS_BASE="$VPS_BASE" \
ADVERTISE_URL="$ADVERTISE_URL" \
ADMIN_TOKEN="$ADMIN_TOKEN" \
P2P_TOKEN="$P2P_TOKEN" \
RUN_AUTOPILOT=0 \
AUTO_START_MINING_WHEN_SYNCED=0 \
bash "$ROOT_DIR/scripts/ops/prefinal_public_sync.sh"

echo "[worker-recover] step 2/4: force local mining OFF"
curl -fsS -X POST "http://127.0.0.1:8080/api/mining/stop" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null 2>&1 || true

echo "[worker-recover] step 3/4: restart worker participant loop"
VPS_SSH="$VPS_SSH" \
VPS_BASE="$VPS_BASE" \
COORD_URL="$COORD_URL" \
ADVERTISE_URL="$ADVERTISE_URL" \
ADMIN_TOKEN="$ADMIN_TOKEN" \
P2P_TOKEN="$P2P_TOKEN" \
WORKER_ID="$WORKER_ID" \
BATCH_SIZE="$BATCH_SIZE" \
HASHRATE_GHS="$HASHRATE_GHS" \
bash "$ROOT_DIR/scripts/ops/worker_mode_participant_up.sh"

echo "[worker-recover] step 4/4: health check"
VPS_BASE="$VPS_BASE" COORD_URL="$COORD_URL" LOCAL_BASE="http://127.0.0.1:8080" \
REQUIRE_WORKER_ACTIVITY=1 \
bash "$ROOT_DIR/scripts/ops/worker_mode_health.sh"

echo "[worker-recover] recovery completed"

