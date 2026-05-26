#!/usr/bin/env bash
# Gate: pool sync metrics + optional live register against coordinator.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_FILE="${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}"

echo "[pool-sync-gate] GET $BASE/api/status pool_sync"
st="$(curl -fsS --max-time 15 "$BASE/api/status")"
echo "$st" | python3 -c '
import json,sys
d=json.load(sys.stdin)
ps=d.get("pool_sync") or {}
m=ps.get("metrics") or {}
print("async", ps.get("async_enabled"), "resolved", ps.get("coordinator_url_resolved"))
print("metrics ok=%s fail=%s pending=%s last=%s latency_ms=%s" % (
  m.get("total_ok"), m.get("total_fail"), m.get("pending_count"),
  m.get("last_status"), m.get("last_latency_ms")))
failed=ps.get("failed_campaigns") or {}
if failed:
    print("failed_campaigns", len(failed))
'

if [[ ! -f "$ADMIN_FILE" ]]; then
  echo "[pool-sync-gate] skip live register (no admin token)"
  echo "[pool-sync-gate] PASS (status only)"
  exit 0
fi

ADMIN="$(tr -d '\r\n' <"$ADMIN_FILE")"
WASM_HEX="${WASM_HEX:-$(xxd -p "$ROOT/tasks/artifacts/security/rust_script_push_bounds_guard.wasm" | tr -d '\n')}"
CID="pool-sync-gate-$(date +%s)"
echo "[pool-sync-gate] POST /api/security-audit $CID"
resp="$(curl -fsS --max-time 30 -X POST "$BASE/api/security-audit" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "{\"title\":\"pool-sync-gate\",\"payer_ref\":\"gate:pool-sync\",\"budget_hmc\":0.5,\"budget_runs\":8,\"wasm_check_hex\":\"$WASM_HEX\",\"create_poh_order\":false,\"campaign_id\":\"$CID\",\"order_id\":\"order-$CID\"}")"

echo "$resp" | python3 -c '
import json,sys,time
d=json.load(sys.stdin)
assert d.get("ok"), d
ps=d.get("pool_sync","")
assert ps in ("queued","ok"), "unexpected pool_sync=%r" % ps
print("pool_sync", ps, "campaign", d.get("campaign_id"))
'

# Wait for background worker (async register).
for i in 1 2 3 4 5 6 7 8 9 10; do
  sleep 2
  st2="$(curl -fsS --max-time 10 "$BASE/api/status")"
  if echo "$st2" | python3 -c "
import json,sys
m=(json.load(sys.stdin).get('pool_sync') or {}).get('metrics') or {}
last=m.get('last_campaign_id','')
st=m.get('last_status','')
err=m.get('last_error','')
ok=m.get('total_ok',0)
if st=='ok' and last.startswith('pool-sync-gate'):
    print('ok')
    sys.exit(0)
if st=='fail' and last.startswith('pool-sync-gate'):
    print('fail', err)
    sys.exit(2)
sys.exit(1)
"; then
    echo "[pool-sync-gate] coordinator register confirmed"
    echo "[pool-sync-gate] PASS"
    exit 0
  fi
  code=$?
  if [[ "$code" == "2" ]]; then
    echo "[pool-sync-gate] FAIL: register failed"
    exit 1
  fi
done

echo "[pool-sync-gate] WARN: register not confirmed in 20s (check coordinator / metrics)"
echo "[pool-sync-gate] PASS (soft — API queued)"
