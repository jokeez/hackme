#!/usr/bin/env bash
set -euo pipefail

# Unified health check for worker-mode deployment.
# Compares VPS canon vs local follower and checks coordinator worker activity.

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[worker-health] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq

VPS_BASE="${VPS_BASE:-https://hackme.tech}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
REQUIRE_WORKER_ACTIVITY="${REQUIRE_WORKER_ACTIVITY:-0}"

vps_status="$(curl -fsS --connect-timeout 5 --max-time 20 "${VPS_BASE}/api/status")"
local_status="$(curl -fsS --connect-timeout 5 --max-time 20 "${LOCAL_BASE}/api/status")"
global_metrics="$(curl -fsS --connect-timeout 5 --max-time 20 "${VPS_BASE}/api/global/metrics")"

# Prefer compact stats to avoid huge payloads on old coordinators with large abuse maps.
work_stats="$(curl -fsS --connect-timeout 5 --max-time 20 "${COORD_URL}/api/work/stats?details=0" 2>/dev/null || true)"
if [[ -z "${work_stats}" ]]; then
  work_stats="$(curl -fsS --connect-timeout 5 --max-time 20 "${COORD_URL}/api/work/stats" 2>/dev/null || true)"
fi

vps_h="$(printf '%s' "$vps_status" | jq -r '.tip_height // 0')"
vps_tip="$(printf '%s' "$vps_status" | jq -r '.tip_hash // ""')"
vps_mining="$(printf '%s' "$vps_status" | jq -r '.mining // false')"
local_h="$(printf '%s' "$local_status" | jq -r '.tip_height // 0')"
local_tip="$(printf '%s' "$local_status" | jq -r '.tip_hash // ""')"
local_mining="$(printf '%s' "$local_status" | jq -r '.mining // false')"
issued="$(printf '%s' "$work_stats" | jq -r '.issued_ranges // .work.issued_ranges // 0' 2>/dev/null || echo 0)"
submitted="$(printf '%s' "$work_stats" | jq -r '.submitted_items // .work.submitted_items // 0' 2>/dev/null || echo 0)"
workers_cnt="$(printf '%s' "$work_stats" | jq -r '.workers_count // (.workers|length) // .work.workers_count // 0' 2>/dev/null || echo 0)"
g_height="$(printf '%s' "$global_metrics" | jq -r '.chain.tip_height // 0')"
g_workers="$(printf '%s' "$global_metrics" | jq -r '.work.workers_count // 0')"
g_issued="$(printf '%s' "$global_metrics" | jq -r '.work.issued_ranges // 0')"
g_submitted="$(printf '%s' "$global_metrics" | jq -r '.work.submitted_items // 0')"
g_source="$(printf '%s' "$global_metrics" | jq -r '.global_source // ""')"

# Fallback when coordinator stats endpoint is unstable or too heavy.
if [[ "${workers_cnt}" == "0" && "${g_workers}" != "0" ]]; then
  workers_cnt="${g_workers}"
fi
if [[ "${issued}" == "0" && "${g_issued}" != "0" ]]; then
  issued="${g_issued}"
fi
if [[ "${submitted}" == "0" && "${g_submitted}" != "0" ]]; then
  submitted="${g_submitted}"
fi

echo "[worker-health] vps   h=${vps_h} mining=${vps_mining} tip=${vps_tip}"
echo "[worker-health] local h=${local_h} mining=${local_mining} tip=${local_tip}"
echo "[worker-health] work  issued=${issued} submitted=${submitted} workers=${workers_cnt}"
echo "[worker-health] global source=${g_source} chain_h=${g_height} workers=${g_workers}"

if [[ -z "$vps_tip" || -z "$local_tip" || "$vps_tip" != "$local_tip" ]]; then
  echo "[worker-health] ERROR: tip mismatch (run prefinal_public_sync.sh)" >&2
  exit 2
fi
if [[ "$local_mining" == "true" ]]; then
  echo "[worker-health] ERROR: local mining must be OFF in worker mode" >&2
  exit 3
fi
if [[ "$vps_mining" != "true" ]]; then
  echo "[worker-health] WARN: VPS mining is OFF (set START_CANON_MINING=1 in hub mode)" >&2
fi
if [[ "$REQUIRE_WORKER_ACTIVITY" == "1" ]]; then
  if [[ "$workers_cnt" == "0" || "$submitted" == "0" ]]; then
    echo "[worker-health] ERROR: worker activity is required but missing" >&2
    exit 4
  fi
fi
if [[ "$g_height" != "$vps_h" ]]; then
  echo "[worker-health] ERROR: global metrics chain height mismatch" >&2
  exit 5
fi
if [[ "$workers_cnt" != "$g_workers" ]]; then
  echo "[worker-health] WARN: global workers_count != coordinator workers (short poll skew possible)" >&2
fi

echo "[worker-health] OK"

