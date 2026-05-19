#!/usr/bin/env bash
set -euo pipefail

# settlement_healthcheck.sh
# Verifies payout settlement loop is healthy and sync assumptions hold.

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[settlement-health] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_SECRET_FILE="${COORD_SECRET_FILE:-${ROOT_DIR}/.secrets/hackme_coordinator_admin_token}"

COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-${HACKME_COORDINATOR_ADMIN_TOKEN:-${COORD_TOKEN:-}}}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
STATE_FILE="${STATE_FILE:-/opt/hackme/data/worker_settlement_state.json}"
MAX_UNSETTLED_HMC="${MAX_UNSETTLED_HMC:-0.5}"
EXPECTED_WALLET_SOURCES="${EXPECTED_WALLET_SOURCES:-canonical_peer,local_db}"
MAX_SWEEP_ETA_SEC="${MAX_SWEEP_ETA_SEC:-93600}" # 26h default safety window

if [[ -z "$COORD_ADMIN_TOKEN" && -r "$COORD_SECRET_FILE" ]]; then
  COORD_ADMIN_TOKEN="$(tr -d '\r\n' <"$COORD_SECRET_FILE")"
fi
if [[ -z "$COORD_ADMIN_TOKEN" ]]; then
  echo "[settlement-health] COORD_ADMIN_TOKEN required for ${COORD_URL}/api/work/stats?details=1" >&2
  exit 1
fi

stats="$(curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: ${COORD_ADMIN_TOKEN}" "${COORD_URL}/api/work/stats?details=1")"
hybrid="$(printf '%s' "$stats" | jq -r '.hybrid_signer_enabled // false')"
workers="$(printf '%s' "$stats" | jq -c '.workers // {}')"

wallet_src="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/wallet" | jq -r '.wallet_source // ""' || true)"
wallet_ok=0
IFS=',' read -r -a expected_sources <<<"$EXPECTED_WALLET_SOURCES"
for src in "${expected_sources[@]}"; do
  if [[ "$(echo "$src" | xargs)" == "$wallet_src" ]]; then
    wallet_ok=1
    break
  fi
done
if [[ "$wallet_ok" != "1" ]]; then
  echo "[settlement-health] ERROR: local wallet_source=${wallet_src} (want one of: ${EXPECTED_WALLET_SOURCES})" >&2
  exit 2
fi

if [[ ! -f "$STATE_FILE" ]]; then
  echo "[settlement-health] ERROR: missing state file ${STATE_FILE}" >&2
  exit 3
fi

state_json="$(jq -c . "$STATE_FILE")"
max_delta="$(jq -r --argjson st "$state_json" '
  . as $ws
  | (to_entries | map(
      (.key) as $wid
      | (.value.payout_hmc // 0) as $p
      | (($st.workers[$wid].settled_hmc // 0) | tonumber) as $s
      | ($p - $s)
    ) | max // 0)
' <<<"$workers")"
max_delta="${max_delta:-0}"

too_high="$(python3 - "$max_delta" "$MAX_UNSETTLED_HMC" <<'PY'
import sys
d=float(sys.argv[1]); m=float(sys.argv[2])
print("1" if d > m else "0")
PY
)"

echo "[settlement-health] hybrid=${hybrid} wallet_source=${wallet_src} max_unsettled_hmc=${max_delta}"
if [[ "$too_high" == "1" ]]; then
  echo "[settlement-health] ERROR: max unsettled HMC ${max_delta} exceeds ${MAX_UNSETTLED_HMC}" >&2
  exit 4
fi

# SLA check from node settlement API (daily sweep cadence visibility).
settle="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/worker/settlement" || true)"
settle_ok="$(printf '%s' "$settle" | jq -r '.ok // false' 2>/dev/null || echo false)"
if [[ "$settle_ok" != "true" ]]; then
  echo "[settlement-health] ERROR: /api/worker/settlement unavailable" >&2
  exit 5
fi
eta_sec="$(printf '%s' "$settle" | jq -r '.daily_sweep_eta_sec // 0' 2>/dev/null || echo 0)"
threshold_ready="$(printf '%s' "$settle" | jq -r '.threshold_ready // false' 2>/dev/null || echo false)"
if [[ -z "$eta_sec" || "$eta_sec" == "null" ]]; then
  eta_sec=0
fi
if ! [[ "$eta_sec" =~ ^[0-9]+$ ]]; then
  eta_sec=0
fi
if (( eta_sec > MAX_SWEEP_ETA_SEC )); then
  echo "[settlement-health] ERROR: daily_sweep_eta_sec=${eta_sec} exceeds MAX_SWEEP_ETA_SEC=${MAX_SWEEP_ETA_SEC}" >&2
  exit 6
fi

echo "[settlement-health] sweep_eta_sec=${eta_sec} threshold_ready=${threshold_ready}"

echo "[settlement-health] OK"
