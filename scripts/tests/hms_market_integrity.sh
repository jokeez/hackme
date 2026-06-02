#!/usr/bin/env bash
# HMS market data-integrity gate: DB invariants, re-upload, repair, capacity.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[hms-integrity] go test internal/hms (integrity + capacity + health)"
go test ./internal/hms/ -count=1 -timeout 180s -run 'Integrity|Capacity|Health|MarketPayment|MarketQuote'

TMP="$(mktemp -d)"
EPHEMERAL_PID=""
COORD_URL=""
cleanup() {
  kill "$EPHEMERAL_PID" 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

export HMS_COORDINATOR_ADDR="127.0.0.1:18095"
export HMS_COORDINATOR_DB="$TMP/hms.db"
export HMS_COORDINATOR_ALLOW_INSECURE=1
export HMS_EPOCH_SECONDS=120
export HMS_FREEZE_AFTER_SEC=90
export HMS_SEAL_WINDOW_SEC=60
export HMS_MARKET_STORAGE_ROOT="$TMP/storage"
export HMS_MARKET_DATA_DIR="$TMP/market"
export HMS_MARKET_SKIP_PAYMENT=1

go build -trimpath -o "$TMP/hmscoordinator" ./cmd/hmscoordinator
mkdir -p "$TMP/storage/w-int"
"$TMP/hmscoordinator" >>"$TMP/coord.log" 2>&1 &
EPHEMERAL_PID=$!
COORD_URL="http://127.0.0.1:18095"
for _ in $(seq 1 60); do
  curl -fsS --max-time 1 "$COORD_URL/health" >/dev/null 2>&1 && break
  sleep 0.25
done

pubkey=$(printf '%64s' | tr ' ' 'b')
curl -fsS -X POST "$COORD_URL/api/storage/register" -H 'Content-Type: application/json' \
  -d "{\"worker_id\":\"w-int\",\"pubkey_hex\":\"$pubkey\",\"quota_gb\":100}" >/dev/null

echo "[hms-integrity] quote includes capacity snapshot"
curl -fsS -X POST "$COORD_URL/api/market/quote" -H 'Content-Type: application/json' \
  -d '{"size_bytes":1048576,"retention_days":30}' | jq -e '.capacity.free_bytes > 0' >/dev/null

echo "[hms-integrity] multi-chunk upload preserves totals"
QUOTE=$(curl -fsS -X POST "$COORD_URL/api/market/quote" -H 'Content-Type: application/json' \
  -d '{"size_bytes":4096,"retention_days":30}')
QH=$(echo "$QUOTE" | jq -r '.quote.quote_hash')
CREATE=$(curl -fsS -X POST "$COORD_URL/api/market/orders" -H 'Content-Type: application/json' \
  -d "{\"label\":\"integrity\",\"client_ref\":\"ci\",\"size_plan_bytes\":4096,\"retention_days\":30,\"quote_hash\":\"$QH\"}")
OID=$(echo "$CREATE" | jq -r '.result.order.order_id')
TOK=$(echo "$CREATE" | jq -r '.result.upload_token')
H1=$(echo -n "aaa" | xxd -p -c 256)
H2=$(echo -n "bbbb" | xxd -p -c 256)
curl -fsS -X POST "$COORD_URL/api/market/orders/$OID/upload" \
  -H "Content-Type: application/json" -H "X-HMS-Upload-Token: $TOK" \
  -d "{\"chunk_index\":0,\"ciphertext_hex\":\"$H1\"}" | jq -e '.ok == true' >/dev/null
curl -fsS -X POST "$COORD_URL/api/market/orders/$OID/upload" \
  -H "Content-Type: application/json" -H "X-HMS-Upload-Token: $TOK" \
  -d "{\"chunk_index\":1,\"ciphertext_hex\":\"$H2\"}" | jq -e '.ok == true' >/dev/null
curl -fsS "$COORD_URL/api/market/orders/$OID" | jq -e '.order.bytes_uploaded == 7 and .order.chunk_count == 2' >/dev/null

echo "[hms-integrity] re-upload same index replaces bytes (not double count)"
H3=$(python3 -c 'print("cc"*20)')
curl -fsS -X POST "$COORD_URL/api/market/orders/$OID/upload" \
  -H "Content-Type: application/json" -H "X-HMS-Upload-Token: $TOK" \
  -d "{\"chunk_index\":0,\"ciphertext_hex\":\"$H3\"}" | jq -e '.ok == true' >/dev/null
curl -fsS "$COORD_URL/api/market/orders/$OID" | jq -e '.order.bytes_uploaded == 24 and .order.chunk_count == 2' >/dev/null

echo "[hms-integrity] health endpoint after complete"
curl -fsS -X POST "$COORD_URL/api/market/orders/$OID/complete" -H "X-HMS-Upload-Token: $TOK" >/dev/null
curl -fsS "$COORD_URL/api/market/orders/$OID/health" | jq -e '.health.restore_ok == true' >/dev/null

echo "[hms-integrity] OK"
