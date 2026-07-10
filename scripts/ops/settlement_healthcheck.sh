#!/usr/bin/env bash
set -euo pipefail

# settlement_healthcheck.sh
# Verifies payout settlement loop is healthy and sync assumptions hold.
#
# Desktop (default when logs/desktop state exists):
#   bash scripts/ops/settlement_healthcheck.sh
# VPS:
#   STATE_FILE=/opt/hackme/data/worker_settlement_state.json \
#   LOCAL_BASE=http://127.0.0.1:18080 COORD_URL=http://127.0.0.1:18081 \
#   bash scripts/ops/settlement_healthcheck.sh

require_cmd() {
  if command -v "$1" 2>&1 | grep -q .; then
    return 0
  fi
  echo "[settlement-health] missing command: $1" >&2
  exit 1
}

require_cmd curl
require_cmd jq

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_SECRET_FILE="${COORD_SECRET_FILE:-${ROOT_DIR}/.secrets/hackme_coordinator_admin_token}"
DESKTOP_STATE="${ROOT_DIR}/logs/desktop/data/worker_settlement_state.json"

COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-${HACKME_COORDINATOR_ADMIN_TOKEN:-${COORD_TOKEN:-}}}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
ADMIN_SECRET_FILE="${ADMIN_SECRET_FILE:-${ROOT_DIR}/.secrets/hackme_admin_token}"
STATE_FILE="${STATE_FILE:-}"
if [[ -z "$STATE_FILE" ]]; then
  if [[ -f "$DESKTOP_STATE" ]]; then
    STATE_FILE="$DESKTOP_STATE"
  else
    STATE_FILE="/opt/hackme/data/worker_settlement_state.json"
  fi
fi

# Desktop worker unpaid between autopilot cycles (~0.5–2 HMC); fleet cap higher for multi-worker hosts.
MAX_WALLET_UNPAID_HMC="${MAX_WALLET_UNPAID_HMC:-3}"
MAX_FLEET_UNPAID_HMC="${MAX_FLEET_UNPAID_HMC:-25}"
# Legacy alias
MAX_UNSETTLED_HMC="${MAX_UNSETTLED_HMC:-$MAX_FLEET_UNPAID_HMC}"
EXPECTED_WALLET_SOURCES="${EXPECTED_WALLET_SOURCES:-canonical_peer,canonical_peer_cache,local_db}"
MAX_SWEEP_ETA_SEC="${MAX_SWEEP_ETA_SEC:-93600}" # 26h default safety window
DESKTOP_WORKER_ID="${DESKTOP_WORKER_ID:-worker-kapa-pc}"

if [[ -z "$COORD_ADMIN_TOKEN" && -r "$COORD_SECRET_FILE" ]]; then
  COORD_ADMIN_TOKEN="$(tr -d '\r\n' <"$COORD_SECRET_FILE")"
fi
if [[ -z "$COORD_ADMIN_TOKEN" ]]; then
  echo "[settlement-health] COORD_ADMIN_TOKEN required for ${COORD_URL}/api/work/stats?details=1" >&2
  exit 1
fi
if [[ -z "$ADMIN_TOKEN" && -r "$ADMIN_SECRET_FILE" ]]; then
  ADMIN_TOKEN="$(tr -d '\r\n' <"$ADMIN_SECRET_FILE")"
fi
if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[settlement-health] ADMIN_TOKEN required for settlement tx probe" >&2
  exit 1
fi

tx_code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 15 -X POST \
  "${CHAIN_BASE}/api/tx/send" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -d '{"tx_type":"transfer_v1","from":"HMC-probe","to":"HMC-probe","amount_units":1,"fee_units":1000,"nonce":0,"timestamp_unix":1}' 2>/dev/null || echo 000)"
if [[ "$tx_code" == "401" ]]; then
  echo "[settlement-health] ERROR: CHAIN_BASE tx/send returned 401 — run sync_settlement_admin_token.sh" >&2
  exit 7
fi

stats="$(curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: ${COORD_ADMIN_TOKEN}" "${COORD_URL}/api/work/stats?details=1")"
hybrid="$(printf '%s' "$stats" | jq -r '.hybrid_signer_enabled // false')"

wallet_hdr=()
if [[ -n "$ADMIN_TOKEN" ]]; then
  wallet_hdr=(-H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}")
fi
wallet_src="$(curl -fsS --max-time 15 "${wallet_hdr[@]}" "${LOCAL_BASE}/api/wallet" | jq -r '.wallet_source // ""' || true)"
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

# Prefer settlement API (same math as dashboard) over raw coord−state for all workers.
settle="$(curl -fsS --max-time 12 "${LOCAL_BASE}/api/worker/settlement?lite=1" || true)"
settle_ok="$(printf '%s' "$settle" | jq -r '.ok // false' 2>/dev/null || echo false)"
wallet_unpaid="0"
fleet_unpaid="0"
if [[ "$settle_ok" == "true" ]]; then
  wallet_unpaid="$(printf '%s' "$settle" | jq -r '.wallet_unpaid_hmc // 0' 2>/dev/null || echo 0)"
  fleet_unpaid="$(printf '%s' "$settle" | jq -r '.fleet_unpaid_hmc // .total_unpaid_hmc // 0' 2>/dev/null || echo 0)"
fi

# Fallback: max unpaid only for payout-mapped / desktop worker rows (skip offline foreign rigs).
state_json="$(jq -c . "$STATE_FILE")"
workers="$(printf '%s' "$stats" | jq -c '.workers // {}')"
max_mapped_delta="$(python3 - "$workers" "$state_json" "$DESKTOP_WORKER_ID" <<'PY'
import json, sys, os
coord = json.loads(sys.argv[1])
state = json.loads(sys.argv[2])
desktop = sys.argv[3]
pmap = {}
raw = os.environ.get("HACKME_WORKER_PAYOUT_MAP") or os.environ.get("WORKER_PAYOUT_MAP") or ""
for part in raw.split(","):
    if "=" in part:
        k, v = part.split("=", 1)
        pmap[k.strip()] = v.strip().lower()
workers = state.get("workers") or {}
max_d = 0.0
for wid, row in coord.items():
    if not isinstance(row, dict):
        continue
    payout = float(row.get("payout_hmc") or 0)
    settled = float((workers.get(wid) or {}).get("settled_hmc") or 0)
    delta = payout - settled
    if delta <= 0:
        continue
    # Only workers on this desktop payout map, or the local desktop worker id.
    if wid == desktop or wid in pmap:
        max_d = max(max_d, delta)
print(max_d)
PY
)"
max_mapped_delta="${max_mapped_delta:-0}"

echo "[settlement-health] hybrid=${hybrid} wallet_source=${wallet_src} wallet_unpaid_hmc=${wallet_unpaid} fleet_unpaid_hmc=${fleet_unpaid} max_mapped_delta=${max_mapped_delta}"

too_wallet="$(python3 - "$wallet_unpaid" "$MAX_WALLET_UNPAID_HMC" <<'PY'
import sys
print("1" if float(sys.argv[1]) > float(sys.argv[2]) else "0")
PY
)"
too_fleet="$(python3 - "$fleet_unpaid" "$MAX_FLEET_UNPAID_HMC" <<'PY'
import sys
print("1" if float(sys.argv[1]) > float(sys.argv[2]) else "0")
PY
)"

if [[ "$too_wallet" == "1" ]]; then
  echo "[settlement-health] ERROR: wallet_unpaid_hmc ${wallet_unpaid} exceeds ${MAX_WALLET_UNPAID_HMC} (run sync_desktop_settlement_canonical.sh or wait for autopilot)" >&2
  exit 4
fi
if [[ "$too_fleet" == "1" ]]; then
  echo "[settlement-health] ERROR: fleet_unpaid_hmc ${fleet_unpaid} exceeds ${MAX_FLEET_UNPAID_HMC}" >&2
  exit 4
fi

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
