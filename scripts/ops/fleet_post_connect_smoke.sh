#!/usr/bin/env bash
set -euo pipefail

# Final smoke after mass worker connect.
# Checks:
# - /api/status
# - /api/work/stats?details=1
# - /api/worker/settlement
# - settlement SLA envelope
#
# Example:
#   COORD_URL=http://127.0.0.1:18081 \
#   LOCAL_BASE=http://127.0.0.1:8080 \
#   CHAIN_BASE=http://127.0.0.1:18080 \
#   MIN_WORKERS=10 \
#   bash scripts/ops/fleet_post_connect_smoke.sh

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[fleet-smoke] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq
require_cmd python3

COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
MIN_WORKERS="${MIN_WORKERS:-1}"
MAX_SWEEP_ETA_SEC="${MAX_SWEEP_ETA_SEC:-93600}"
MAX_UNPAID_HMC="${MAX_UNPAID_HMC:-2.0}"

status="$(curl -fsS --max-time 15 "${CHAIN_BASE}/api/status")"
work="$(curl -fsS --max-time 15 "${COORD_URL}/api/work/stats?details=1")"
settle="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/worker/settlement")"

tip_height="$(printf '%s' "$status" | jq -r '.tip_height // 0')"
tip_hash="$(printf '%s' "$status" | jq -r '.tip_hash // ""')"
mining="$(printf '%s' "$status" | jq -r '.mining // false')"
workers_count="$(printf '%s' "$work" | jq -r '(.workers // {} | length)')"
attempts_total="$(printf '%s' "$work" | jq -r '.attempts_total // 0')"
found_total="$(printf '%s' "$work" | jq -r '.found_total // 0')"
unpaid_hmc="$(printf '%s' "$settle" | jq -r '.total_unpaid_hmc // 0')"
eta_sec="$(printf '%s' "$settle" | jq -r '.daily_sweep_eta_sec // 0')"
threshold_ready="$(printf '%s' "$settle" | jq -r '.threshold_ready // false')"
settle_ok="$(printf '%s' "$settle" | jq -r '.ok // false')"

echo "[fleet-smoke] status tip_height=${tip_height} tip_hash=${tip_hash} mining=${mining}"
echo "[fleet-smoke] work workers=${workers_count} attempts_total=${attempts_total} found_total=${found_total}"
echo "[fleet-smoke] settlement ok=${settle_ok} unpaid_hmc=${unpaid_hmc} eta_sec=${eta_sec} threshold_ready=${threshold_ready}"

if [[ "$settle_ok" != "true" ]]; then
  echo "[fleet-smoke] ERROR: /api/worker/settlement returned ok=false" >&2
  exit 2
fi
if ! [[ "$workers_count" =~ ^[0-9]+$ ]] || (( workers_count < MIN_WORKERS )); then
  echo "[fleet-smoke] ERROR: workers_count=${workers_count} < MIN_WORKERS=${MIN_WORKERS}" >&2
  exit 3
fi
if ! [[ "$eta_sec" =~ ^[0-9]+$ ]] || (( eta_sec > MAX_SWEEP_ETA_SEC )); then
  echo "[fleet-smoke] ERROR: daily_sweep_eta_sec=${eta_sec} > MAX_SWEEP_ETA_SEC=${MAX_SWEEP_ETA_SEC}" >&2
  exit 4
fi

too_high="$(
  python3 - "$unpaid_hmc" "$MAX_UNPAID_HMC" <<'PY'
import sys
u = float(sys.argv[1])
m = float(sys.argv[2])
print("1" if u > m else "0")
PY
)"
if [[ "$too_high" == "1" ]]; then
  echo "[fleet-smoke] ERROR: total_unpaid_hmc=${unpaid_hmc} > MAX_UNPAID_HMC=${MAX_UNPAID_HMC}" >&2
  exit 5
fi

echo "[fleet-smoke] PASS"
