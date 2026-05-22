#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

VPS_BASE="${VPS_BASE:-http://hackme-vps:18080}"
COORD_URL="${COORD_URL:-http://hackme-vps:18081}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"

echo "[worker-mine-status] local node:"
curl -fsS "${LOCAL_BASE}/api/status" | jq '{tip_height,tip_hash,mining,node_address}'
echo "[worker-mine-status] vps canonical:"
curl -fsS "${VPS_BASE}/api/status" | jq '{tip_height,tip_hash,mining,node_address}'
echo "[worker-mine-status] coordinator:"
curl -fsS "${COORD_URL}/api/work/stats?details=0" | jq '{issued_ranges,submitted_items,workers_count,active_leases_count,total_payout_hmc}'

echo "[worker-mine-status] health:"
VPS_BASE="${VPS_BASE}" COORD_URL="${COORD_URL}" LOCAL_BASE="${LOCAL_BASE}" REQUIRE_WORKER_ACTIVITY=1 \
  bash "$ROOT_DIR/scripts/ops/worker_mode_health.sh"
