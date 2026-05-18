#!/usr/bin/env bash
set -euo pipefail

# Quick consistency smoke between local follower and canonical VPS node.
# Verifies tip alignment, address mirrors, tx lookup, and worker health.
#
# Usage:
#   LOCAL_BASE=http://127.0.0.1:8080 VPS_BASE=http://132.243.112.100:18080 \
#   bash scripts/ops/canonical_consistency_smoke.sh
#
# Optional:
#   CHECK_TX_HASH=<tx_hash>            # verify tx lookup on local mirror
#   CHECK_ADDRESS=HMC-...              # extra address to compare

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[canon-smoke] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq
require_cmd bash

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
VPS_BASE="${VPS_BASE:-http://132.243.112.100:18080}"
COORD_URL="${COORD_URL:-http://132.243.112.100:18081}"
CHECK_TX_HASH="${CHECK_TX_HASH:-}"
CHECK_ADDRESS="${CHECK_ADDRESS:-HMC-381c0c5e2cfcc714}"
# Optional extra address comparison. Keep empty by default to avoid false negatives:
# local operator wallets are not required to exist on the canonical VPS ledger.
DEV_ADDRESS="${DEV_ADDRESS:-}"

echo "[canon-smoke] fetch status snapshots"
local_status="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/status")"
vps_status="$(curl -fsS --max-time 15 "${VPS_BASE}/api/status")"

local_tip="$(printf '%s' "$local_status" | jq -r '.tip_hash // ""')"
vps_tip="$(printf '%s' "$vps_status" | jq -r '.tip_hash // ""')"
local_h="$(printf '%s' "$local_status" | jq -r '.tip_height // 0')"
vps_h="$(printf '%s' "$vps_status" | jq -r '.tip_height // 0')"
local_src="$(printf '%s' "$local_status" | jq -r '.tip_sync_source // ""')"
vps_mining="$(printf '%s' "$vps_status" | jq -r '.mining // false')"

echo "[canon-smoke] local h=${local_h} tip=${local_tip} src=${local_src}"
echo "[canon-smoke] vps   h=${vps_h} tip=${vps_tip} mining=${vps_mining}"

if [[ -z "$local_tip" || -z "$vps_tip" || "$local_tip" != "$vps_tip" ]]; then
  echo "[canon-smoke] ERROR: tip mismatch local vs VPS" >&2
  exit 2
fi

echo "[canon-smoke] compare key addresses"
addresses=("$CHECK_ADDRESS")
if [[ -n "$DEV_ADDRESS" ]]; then
  addresses+=("$DEV_ADDRESS")
fi
for addr in "${addresses[@]}"; do
  l="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/address/${addr}")"
  r="$(curl -fsS --max-time 15 "${VPS_BASE}/api/address/${addr}")"
  l_bal="$(printf '%s' "$l" | jq -r '.balance_units // -1')"
  r_bal="$(printf '%s' "$r" | jq -r '.balance_units // -1')"
  l_nonce="$(printf '%s' "$l" | jq -r '.next_nonce // -1')"
  r_nonce="$(printf '%s' "$r" | jq -r '.next_nonce // -1')"
  echo "[canon-smoke] ${addr} local(balance=${l_bal},nonce=${l_nonce}) vps(balance=${r_bal},nonce=${r_nonce})"
  if [[ "$l_bal" != "$r_bal" || "$l_nonce" != "$r_nonce" ]]; then
    echo "[canon-smoke] ERROR: address mismatch for ${addr}" >&2
    exit 3
  fi
done

echo "[canon-smoke] compare tx pool shape"
local_pool="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/tx/pool" | jq -cS '.')"
vps_pool="$(curl -fsS --max-time 15 "${VPS_BASE}/api/tx/pool" | jq -cS '.')"
if [[ "$local_pool" != "$vps_pool" ]]; then
  echo "[canon-smoke] ERROR: tx pool mismatch local vs VPS" >&2
  exit 4
fi

if [[ -n "$CHECK_TX_HASH" ]]; then
  echo "[canon-smoke] verify tx hash ${CHECK_TX_HASH}"
  local_tx="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/tx/${CHECK_TX_HASH}" | jq -cS '.')"
  vps_tx="$(curl -fsS --max-time 15 "${VPS_BASE}/api/tx/${CHECK_TX_HASH}" | jq -cS '.')"
  if [[ "$local_tx" != "$vps_tx" ]]; then
    echo "[canon-smoke] ERROR: tx lookup mismatch for ${CHECK_TX_HASH}" >&2
    exit 5
  fi
fi

echo "[canon-smoke] run worker-mode health"
VPS_BASE="$VPS_BASE" COORD_URL="$COORD_URL" LOCAL_BASE="$LOCAL_BASE" \
  bash "$ROOT_DIR/scripts/ops/worker_mode_health.sh"

echo "[canon-smoke] OK"
