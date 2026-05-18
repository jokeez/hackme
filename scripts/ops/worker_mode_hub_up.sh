#!/usr/bin/env bash
set -euo pipefail

# VPS-side helper for stable "hub + coordinator" mode:
# - keep chain canon on VPS node
# - run coordinator for remote workers
# - leave mining OFF by default (optional toggle)

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[worker-hub-up] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd curl
require_cmd jq
require_cmd go
require_cmd nohup
require_cmd ss

MAIN_BASE="${MAIN_BASE:-http://127.0.0.1:18080}"
COORD_ADDR="${COORD_ADDR:-0.0.0.0:18081}"
COORD_BASE="${COORD_BASE:-http://127.0.0.1:18081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
START_CANON_MINING="${START_CANON_MINING:-0}"
COORD_LOG="${COORD_LOG:-$ROOT_DIR/logs/coordinator_hub.log}"
COORD_DB="${COORD_DB:-$ROOT_DIR/data/coordinator.db}"
COORD_ORDERS_URL="${COORD_ORDERS_URL:-${MAIN_BASE}}"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[worker-hub-up] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 1
fi

echo "[worker-hub-up] checking VPS main node at ${MAIN_BASE}"
if ! curl -fsS "${MAIN_BASE}/api/status" >/dev/null 2>&1; then
  echo "[worker-hub-up] ERROR: main node is not reachable at ${MAIN_BASE}" >&2
  echo "[worker-hub-up] tip: ensure systemd hackme-node is running first" >&2
  exit 1
fi

echo "[worker-hub-up] forcing mining OFF on local participant API if present (best-effort)"
curl -fsS -X POST "http://127.0.0.1:8080/api/mining/stop" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null 2>&1 || true

echo "[worker-hub-up] stopping canon mining before mode switch (best-effort)"
curl -fsS -X POST "${MAIN_BASE}/api/mining/stop" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null 2>&1 || true

mkdir -p "$(dirname "$COORD_LOG")" "$(dirname "$COORD_DB")"
pkill -f "go run ./cmd/coordinator" >/dev/null 2>&1 || true
sleep 1

echo "[worker-hub-up] starting coordinator on ${COORD_ADDR}"
nohup env \
  HACKME_COORDINATOR_ADDR="${COORD_ADDR}" \
  HACKME_COORDINATOR_ADMIN_TOKEN="${ADMIN_TOKEN}" \
  HACKME_COORDINATOR_DB="${COORD_DB}" \
  HACKME_COORDINATOR_ORDERS_URL="${COORD_ORDERS_URL}" \
  HACKME_COORDINATOR_ORDERS_PRIORITY=1 \
  go run ./cmd/coordinator >"${COORD_LOG}" 2>&1 &

sleep 2
if ! curl -fsS "${COORD_BASE}/api/network/stats" >/dev/null 2>&1; then
  echo "[worker-hub-up] ERROR: coordinator failed health-check at ${COORD_BASE}" >&2
  echo "[worker-hub-up] check log: ${COORD_LOG}" >&2
  exit 2
fi

if [[ "$START_CANON_MINING" == "1" ]]; then
  echo "[worker-hub-up] enabling canon mining on VPS (command node must run with HACKME_CHAIN_LEADER_LOCAL_POH=1)"
  curl -fsS -X POST "${MAIN_BASE}/api/mining/start" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null
fi

echo "[worker-hub-up] status:"
curl -sS "${MAIN_BASE}/api/status" | jq '{has_genesis,tip_height,tip_hash,node_address,mining}'
curl -sS "${COORD_BASE}/api/work/stats" | jq '{ok,summary:(.summary // {}),coordinator:(.coordinator // "up")}' || true
echo "[worker-hub-up] coordinator log: ${COORD_LOG}"
echo "[worker-hub-up] done"

