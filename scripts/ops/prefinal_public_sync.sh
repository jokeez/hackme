#!/usr/bin/env bash
set -euo pipefail

# One-command prefinal sync flow for public-network staging:
# - freezes mining (local + VPS),
# - refreshes local follower state from live VPS snapshot,
# - verifies tip hash/height alignment.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[prefinal-sync] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq
require_cmd bash
require_cmd ssh
require_cmd ss

VPS_SSH="${VPS_SSH:-}"
VPS_BASE="${VPS_BASE:-http://hackme-vps:18080}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
ADVERTISE_URL="${ADVERTISE_URL:-}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}"
RUN_AUTOPILOT="${RUN_AUTOPILOT:-0}"
AUTO_START_MINING_WHEN_SYNCED="${AUTO_START_MINING_WHEN_SYNCED:-0}"
LOCAL_BIND_PORT="${LOCAL_BIND_PORT:-8080}"

if [[ -z "$VPS_SSH" ]]; then
  echo "[prefinal-sync] VPS_SSH is required (e.g. hackme-vps)" >&2
  exit 1
fi
if [[ -z "$ADVERTISE_URL" ]]; then
  echo "[prefinal-sync] ADVERTISE_URL is required (e.g. http://192.168.1.113:8080)" >&2
  exit 1
fi
if [[ -z "$ADMIN_TOKEN" || -z "$P2P_TOKEN" ]]; then
  echo "[prefinal-sync] ADMIN_TOKEN and P2P_TOKEN are required" >&2
  exit 1
fi

echo "[prefinal-sync] freeze mining on local + VPS"
curl -fsS -X POST "${LOCAL_BASE}/api/mining/stop" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null 2>&1 || true
ssh "$VPS_SSH" "curl -fsS -X POST '${VPS_BASE}/api/mining/stop' -H 'X-Hackme-Admin-Token: ${ADMIN_TOKEN}' >/dev/null 2>&1 || true" || true

echo "[prefinal-sync] stop local processes"
if command -v fuser >/dev/null 2>&1; then
  fuser -k "${LOCAL_BIND_PORT}/tcp" >/dev/null 2>&1 || true
fi
if ss -ltnp 2>/dev/null | awk -v p=":${LOCAL_BIND_PORT}" '$4 ~ (p "$") {found=1} END{exit (found?0:1)}'; then
  pids="$(ss -ltnp 2>/dev/null | awk -v p=":${LOCAL_BIND_PORT}" '$4 ~ (p "$") {print $NF}' | sed -E 's/.*pid=([0-9]+).*/\1/' | tr '\n' ' ')"
  for pid in $pids; do
    kill "$pid" >/dev/null 2>&1 || true
  done
fi
pkill -f hackme-node >/dev/null 2>&1 || true
pkill -f "hackme" >/dev/null 2>&1 || true
pkill -f "go run ." >/dev/null 2>&1 || true
pkill -f p2p_autopilot.sh >/dev/null 2>&1 || true
pkill -f demo_participant_up.sh >/dev/null 2>&1 || true

if ss -ltnp 2>/dev/null | awk -v p=":${LOCAL_BIND_PORT}" '$4 ~ (p "$") {found=1} END{exit (found?0:1)}'; then
  echo "[prefinal-sync] ERROR: local port ${LOCAL_BIND_PORT} still in use after cleanup" >&2
  ss -ltnp 2>/dev/null | awk -v p=":${LOCAL_BIND_PORT}" '$4 ~ (p "$") {print}'
  exit 1
fi

echo "[prefinal-sync] bootstrap follower from fresh VPS snapshot"
VPS_SSH="$VPS_SSH" \
LEADER_URL="$VPS_BASE" \
ADVERTISE_URL="$ADVERTISE_URL" \
ADMIN_TOKEN="$ADMIN_TOKEN" \
P2P_TOKEN="$P2P_TOKEN" \
RUN_AUTOPILOT="$RUN_AUTOPILOT" \
AUTO_START_MINING_WHEN_SYNCED="$AUTO_START_MINING_WHEN_SYNCED" \
bash "$ROOT_DIR/scripts/ops/follower_bootstrap_from_vps.sh"

echo "[prefinal-sync] verify alignment"
local_status="$(curl -fsS "${LOCAL_BASE}/api/status")"
vps_status="$(curl -fsS "${VPS_BASE}/api/status")"
local_tip="$(printf '%s' "$local_status" | jq -r '.tip_hash // ""')"
vps_tip="$(printf '%s' "$vps_status" | jq -r '.tip_hash // ""')"
local_h="$(printf '%s' "$local_status" | jq -r '.tip_height // 0')"
vps_h="$(printf '%s' "$vps_status" | jq -r '.tip_height // 0')"

echo "[prefinal-sync] local: h=${local_h} tip=${local_tip}"
echo "[prefinal-sync] vps:   h=${vps_h} tip=${vps_tip}"
if [[ -z "$local_tip" || -z "$vps_tip" || "$local_tip" != "$vps_tip" ]]; then
  echo "[prefinal-sync] ERROR: tip mismatch after bootstrap" >&2
  exit 2
fi
echo "[prefinal-sync] OK: network aligned (mining remains OFF)"

