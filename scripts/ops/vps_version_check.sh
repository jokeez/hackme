#!/usr/bin/env bash
set -euo pipefail

# Read-only health/version probe for node + coordinator.
#
# Usage:
#   NODE_BASE=http://hackme-vps:18080 \
#   COORD_BASE=http://hackme-vps:18081 \
#   bash scripts/ops/vps_version_check.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

NODE_BASE="${NODE_BASE:-http://127.0.0.1:8080}"
COORD_BASE="${COORD_BASE:-http://127.0.0.1:8081}"

echo "[vps-check] node=${NODE_BASE} coordinator=${COORD_BASE}"

status_json="$(curl -fsS "${NODE_BASE}/api/status")"
metrics_json="$(curl -fsS "${NODE_BASE}/api/metrics")"
work_json="$(curl -fsS "${COORD_BASE}/api/work/stats")"
net_json="$(curl -fsS "${COORD_BASE}/api/network/stats")"

echo "[vps-check] node summary"
jq -n \
  --argjson s "$status_json" \
  --argjson m "$metrics_json" \
  '{
    chain_id: $s.chain_id,
    version: ($s.version // "unknown"),
    commit: ($s.commit // "unknown"),
    build_date: ($s.build_date // "unknown"),
    tip_height: ($s.tip_height // 0),
    mining: ($s.mining // false),
    network_mode_active: ($s.network_mode_active // false),
    target_mod: ($m.mining_target_mod // 0),
    base_reward_hmc: ($m.econ_base_reward_now_hmc // 0)
  }'

echo "[vps-check] coordinator summary"
jq -n \
  --argjson w "$work_json" \
  --argjson n "$net_json" \
  '{
    workers_count: ($w.workers_count // 0),
    issued_ranges: ($w.issued_ranges // 0),
    submitted_items: ($w.submitted_items // 0),
    accepted_attempts: ($w.accepted_attempts // 0),
    target_mod: ($w.target_mod // 0),
    total_payout_hmc: ($w.total_payout_hmc // 0),
    total_miners: ($n.total_miners // 0),
    global_hashrate_th_s: ($n.global_hashrate_th_s // 0),
    peer_connections: ($n.peer_connections // 0)
  }'

echo "[vps-check] PASS"
