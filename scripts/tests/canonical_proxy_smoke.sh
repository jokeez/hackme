#!/usr/bin/env bash
set -euo pipefail

# canonical_proxy_smoke.sh
# Checks follower/worker node API proxy consistency against canonical node.
#
# Usage:
#   LOCAL_BASE=http://127.0.0.1:8080 VPS_BASE=http://<vps-ip>:18080 \
#   bash scripts/tests/canonical_proxy_smoke.sh
#
# Optional:
#   CHECK_ADDRESS=HMC-...
#   CHECK_TX_HASH=<txhash>
#   REQUIRE_WALLET_SOURCE=1   # require local /api/wallet to report canonical_peer

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[canonical-proxy-smoke] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq

LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
VPS_BASE="${VPS_BASE:-http://127.0.0.1:18080}"
CHECK_ADDRESS="${CHECK_ADDRESS:-}"
CHECK_TX_HASH="${CHECK_TX_HASH:-}"
REQUIRE_WALLET_SOURCE="${REQUIRE_WALLET_SOURCE:-0}"

echo "[canonical-proxy-smoke] status parity"
local_status="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/status")"
vps_status="$(curl -fsS --max-time 15 "${VPS_BASE}/api/status")"

local_tip="$(printf '%s' "$local_status" | jq -r '.tip_hash // ""')"
vps_tip="$(printf '%s' "$vps_status" | jq -r '.tip_hash // ""')"
if [[ -z "$local_tip" || -z "$vps_tip" || "$local_tip" != "$vps_tip" ]]; then
  echo "[canonical-proxy-smoke] ERROR: tip_hash mismatch local=${local_tip} canonical=${vps_tip}" >&2
  exit 2
fi

echo "[canonical-proxy-smoke] tx pool parity"
tx_pool_ok=0
for attempt in 1 2 3; do
  local_pool="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/tx/pool" | jq -cS '.')"
  vps_pool="$(curl -fsS --max-time 15 "${VPS_BASE}/api/tx/pool" | jq -cS '.')"
  if [[ "$local_pool" == "$vps_pool" ]]; then
    tx_pool_ok=1
    break
  fi
  sleep 1
done
if [[ "$tx_pool_ok" != "1" ]]; then
  echo "[canonical-proxy-smoke] ERROR: /api/tx/pool mismatch after retries" >&2
  exit 3
fi

echo "[canonical-proxy-smoke] wallet source + parity"
local_wallet="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/wallet")"
vps_wallet="$(curl -fsS --max-time 15 "${VPS_BASE}/api/wallet")"
local_addr="$(printf '%s' "$local_wallet" | jq -r '.address // ""')"
local_wallet_source="$(printf '%s' "$local_wallet" | jq -r '.wallet_source // ""')"
local_units="$(printf '%s' "$local_wallet" | jq -r '.balance_units // -1')"
local_nonce="$(printf '%s' "$local_wallet" | jq -r '.next_nonce // -1')"
vps_addr="$(printf '%s' "$vps_wallet" | jq -r '.address // ""')"
if [[ -n "$local_addr" && "$local_addr" == "$vps_addr" ]]; then
  vps_units="$(printf '%s' "$vps_wallet" | jq -r '.balance_units // -1')"
  vps_nonce="$(printf '%s' "$vps_wallet" | jq -r '.next_nonce // -1')"
  if [[ "$local_units" != "$vps_units" || "$local_nonce" != "$vps_nonce" ]]; then
    echo "[canonical-proxy-smoke] ERROR: /api/wallet mismatch for ${local_addr}" >&2
    exit 4
  fi
fi
if [[ "$REQUIRE_WALLET_SOURCE" == "1" && "$local_wallet_source" != "canonical_peer" ]]; then
  echo "[canonical-proxy-smoke] ERROR: wallet_source=${local_wallet_source} (want canonical_peer)" >&2
  exit 5
fi

if [[ -n "$CHECK_ADDRESS" ]]; then
  echo "[canonical-proxy-smoke] address parity (${CHECK_ADDRESS})"
  local_addr_json="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/address/${CHECK_ADDRESS}")"
  vps_addr_json="$(curl -fsS --max-time 15 "${VPS_BASE}/api/address/${CHECK_ADDRESS}")"
  local_addr_cmp="$(printf '%s' "$local_addr_json" | jq -cS '.')"
  vps_addr_cmp="$(printf '%s' "$vps_addr_json" | jq -cS '.')"
  if [[ "$local_addr_cmp" != "$vps_addr_cmp" ]]; then
    echo "[canonical-proxy-smoke] ERROR: /api/address mismatch for ${CHECK_ADDRESS}" >&2
    exit 6
  fi
fi

if [[ -n "$CHECK_TX_HASH" ]]; then
  echo "[canonical-proxy-smoke] tx parity (${CHECK_TX_HASH})"
  local_tx="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/tx/${CHECK_TX_HASH}" | jq -cS '.')"
  vps_tx="$(curl -fsS --max-time 15 "${VPS_BASE}/api/tx/${CHECK_TX_HASH}" | jq -cS '.')"
  if [[ "$local_tx" != "$vps_tx" ]]; then
    echo "[canonical-proxy-smoke] ERROR: /api/tx hash mismatch for ${CHECK_TX_HASH}" >&2
    exit 7
  fi
fi

echo "[canonical-proxy-smoke] OK"
