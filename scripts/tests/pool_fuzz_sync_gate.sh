#!/usr/bin/env bash
# Gate: node pool fuzz campaign registers on prod/loopback coordinator (no timeout).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_FILE="${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}"
ADMIN="$(tr -d '\r\n' <"$ADMIN_FILE" 2>/dev/null || true)"
COORD="${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}"
COORD="${COORD%/}"
COORD_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)"

require_cmd curl
require_cmd jq

if [[ -z "$ADMIN" ]]; then
  echo "[pool-sync-gate] missing admin token" >&2
  exit 2
fi
if [[ -z "$COORD_ADMIN" ]]; then
  echo "[pool-sync-gate] missing .secrets/hackme_coordinator_admin_token" >&2
  exit 2
fi
if ! curl -fsS --max-time 8 "${BASE}/api/status?lite=1" >/dev/null 2>&1; then
  echo "[pool-sync-gate] node down at $BASE" >&2
  exit 1
fi

WASM_HEX="$(xxd -p "$ROOT/tasks/artifacts/security/rust_script_push_bounds_guard.wasm" | tr -d '\n')"
CID="pool-sync-gate-$(date +%s)"
echo "[pool-sync-gate] POST coordinator register $CID"
curl -fsS --max-time 60 -X POST "${COORD}/api/fuzz/pool/campaigns" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $COORD_ADMIN" \
  -d "$(jq -nc --arg id "$CID" --arg hex "$WASM_HEX" \
    '{id:$id,campaign_type:"property",title:"pool sync gate",status:"running",budget_runs:8,budget_seconds:120,
      config:{wasm_check_hex:$hex,pool_distributed:true}}')" \
  | jq -e '.ok == true and .campaign_id != ""' >/dev/null

echo "[pool-sync-gate] POST /api/security-audit pool_distributed=true"
resp="$(curl -fsS --max-time 60 -X POST "${BASE}/api/security-audit" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "$(jq -nc --arg hex "$WASM_HEX" --arg cid "pool-sync-node-${CID}" \
    '{title:"pool sync node gate",payer_ref:"gate:pool-sync",campaign_id:$cid,order_id:("order-"+$cid),
      budget_hmc:1,budget_runs:8,budget_seconds:120,use_sup_discount:false,wasm_check_hex:$hex,
      create_poh_order:false,pool_distributed:true}')")"
echo "$resp" | jq -e '.ok and (.pool_sync == "ok" or .pool_sync == "queued")' >/dev/null
node_cid="$(echo "$resp" | jq -r '.campaign_id')"
sleep 8
st="$(curl -fsS "${BASE}/api/status" | jq -r --arg c "$node_cid" '.pool_sync.failed_campaigns[$c] // empty')"
if [[ -n "$st" ]]; then
  echo "[pool-sync-gate] pool_sync failed for $node_cid: $st" >&2
  exit 1
fi
echo "[pool-sync-gate] PASS (coordinator + node pool_sync ok for $node_cid)"
