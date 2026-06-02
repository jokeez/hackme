#!/usr/bin/env bash
# HMS market API gate: coordinator + storage worker + create/upload/list.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[hms-market-gate] go test internal/hms (market+capacity+health)"
go test ./internal/hms/ -count=1 -timeout 120s -run 'Market|Capacity|Health|TestBestMount'

TMP="$(mktemp -d)"
EPHEMERAL_PID=""
COORD_URL=""
cleanup_ephemeral() {
  kill "$EPHEMERAL_PID" 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup_ephemeral EXIT

start_ephemeral_coordinator() {
  echo "[hms-market-gate] starting ephemeral coordinator" >&2
  export HMS_COORDINATOR_ADDR="127.0.0.1:18093"
  export HMS_COORDINATOR_DB="$TMP/hms.db"
  export HMS_COORDINATOR_ALLOW_INSECURE=1
  export HMS_EPOCH_SECONDS=120
  export HMS_FREEZE_AFTER_SEC=90
  export HMS_SEAL_WINDOW_SEC=60
  export HMS_MARKET_STORAGE_ROOT="$TMP/storage"
  export HMS_MARKET_DATA_DIR="$TMP/market"
  export HMS_MARKET_SKIP_PAYMENT=1
  go build -trimpath -o "$TMP/hmscoordinator" ./cmd/hmscoordinator
  mkdir -p "$TMP/storage/w-gate"
  "$TMP/hmscoordinator" >>"$TMP/coord.log" 2>&1 &
  EPHEMERAL_PID=$!
  local url="http://127.0.0.1:18093"
  for _ in $(seq 1 60); do
    curl -fsS --max-time 1 "$url/health" >/dev/null 2>&1 && break
    sleep 0.25
  done
  local pubkey
  pubkey=$(printf '%64s' | tr ' ' 'a')
  curl -fsS -X POST "$url/api/storage/register" -H 'Content-Type: application/json' \
    -d "{\"worker_id\":\"w-gate\",\"pubkey_hex\":\"$pubkey\",\"quota_gb\":100}" >/dev/null
  curl -fsS "$url/api/market/capacity" | jq -e '.capacity.online_workers >= 1 and .capacity.free_bytes > 0' >/dev/null
  COORD_URL="$url"
}

run_market_flow() {
  local coord_url="$1"
  QUOTE=$(curl -fsS -X POST "$coord_url/api/market/quote" -H 'Content-Type: application/json' \
    -d '{"size_bytes":1048576,"retention_days":30}')
  QH=$(echo "$QUOTE" | jq -r '.quote.quote_hash')
  CREATE=$(curl -fsS -X POST "$coord_url/api/market/orders" -H 'Content-Type: application/json' \
    -d "{\"label\":\"gate-test\",\"client_ref\":\"ci\",\"size_plan_bytes\":1048576,\"retention_days\":30,\"quote_hash\":\"$QH\"}")
  OID=$(echo "$CREATE" | jq -r '.result.order.order_id')
  TOK=$(echo "$CREATE" | jq -r '.result.upload_token')
  test -n "$OID" && test "$OID" != null
  test -n "$TOK" && test "$TOK" != null

  CT=$(printf 'encrypted-payload-gate')
  HEX=$(echo -n "$CT" | xxd -p -c 256)
  curl -fsS -X POST "$coord_url/api/market/orders/$OID/upload" \
    -H "Content-Type: application/json" -H "X-HMS-Upload-Token: $TOK" \
    -d "{\"chunk_index\":0,\"ciphertext_hex\":\"$HEX\"}" | jq -e '.ok == true' >/dev/null

  curl -fsS -X POST "$coord_url/api/market/orders/$OID/complete" -H "X-HMS-Upload-Token: $TOK" | jq -e '.status == "stored" or .status == "degraded"' >/dev/null
  curl -fsS "$coord_url/api/market/orders/$OID/health" | jq -e '.health.restore_ok == true' >/dev/null
  N=$(curl -fsS "$coord_url/api/market/orders" | jq '.orders | length')
  test "$N" -ge 1
  echo "[hms-market-gate] OK orders=$N oid=$OID url=$coord_url"
}

LIVE_URL="${HACKME_HMS_COORDINATOR_URL:-http://127.0.0.1:18082}"
if curl -fsS --max-time 2 "$LIVE_URL/api/market/stats" >/dev/null 2>&1; then
  echo "[hms-market-gate] trying live coordinator"
  if run_market_flow "$LIVE_URL" 2>/dev/null; then
    exit 0
  fi
  echo "[hms-market-gate] live coordinator unavailable for uploads (epoch frozen?) — ephemeral fallback" >&2
else
  if curl -fsS --max-time 2 "$LIVE_URL/health" >/dev/null 2>&1; then
    echo "[hms-market-gate] WARN: coordinator up but no /api/market — rebuild hmscoordinator" >&2
  fi
fi

start_ephemeral_coordinator
run_market_flow "$COORD_URL"
