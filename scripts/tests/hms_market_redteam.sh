#!/usr/bin/env bash
# HMS market red-team: abuse, token auth, quote tamper, size cap (isolated coordinator).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export HMS_COORDINATOR_ADDR="127.0.0.1:18094"
export HMS_COORDINATOR_DB="$TMP/hms.db"
export HMS_COORDINATOR_ALLOW_INSECURE=1
export HMS_EPOCH_SECONDS=120
export HMS_FREEZE_AFTER_SEC=90
export HMS_SEAL_WINDOW_SEC=60
export HMS_MARKET_STORAGE_ROOT="$TMP/storage"
export HMS_MARKET_DATA_DIR="$TMP/market"
export HMS_MARKET_SKIP_PAYMENT=1

go build -trimpath -o "$TMP/hmscoordinator" ./cmd/hmscoordinator
mkdir -p "$TMP/storage/w-red"
"$TMP/hmscoordinator" >>"$TMP/coord.log" 2>&1 &
CPID=$!
trap 'kill $CPID 2>/dev/null; rm -rf "$TMP"' EXIT
COORD="http://127.0.0.1:18094"
for _ in $(seq 1 40); do
  curl -fsS --max-time 1 "$COORD/health" >/dev/null 2>&1 && break
  sleep 0.15
done

curl -fsS -X POST "$COORD/api/storage/register" -H 'Content-Type: application/json' \
  -d '{"worker_id":"w-red","pubkey_hex":"'$(printf 'cd%.0s' {1..64})'","quota_gb":100}' >/dev/null

echo "[hms-redteam] go unit abuse tests"
go test ./internal/hms/ -count=1 -timeout 120s -run 'MarketUploadExceeds|MarketDownloadRequires|MarketPaymentReplay|MarketQuoteTamper|MarketUploadRequires'

echo "[hms-redteam] wrong upload token"
QUOTE=$(curl -fsS -X POST "$COORD/api/market/quote" -H 'Content-Type: application/json' \
  -d '{"size_bytes":512,"retention_days":30}')
QH=$(echo "$QUOTE" | jq -r '.quote.quote_hash')
CREATE=$(curl -fsS -X POST "$COORD/api/market/orders" -H 'Content-Type: application/json' \
  -d "{\"label\":\"red\",\"client_ref\":\"rt\",\"size_plan_bytes\":512,\"retention_days\":30,\"quote_hash\":\"$QH\"}")
OID=$(echo "$CREATE" | jq -r '.result.order.order_id')
TOK=$(echo "$CREATE" | jq -r '.result.upload_token')
HEX=$(echo -n "payload" | xxd -p -c 256)
curl -fsS -X POST "$COORD/api/market/orders/$OID/upload" \
  -H "Content-Type: application/json" -H "X-HMS-Upload-Token: wrong" \
  -d "{\"chunk_index\":0,\"ciphertext_hex\":\"$HEX\"}" 2>/dev/null && exit 1 || true

echo "[hms-redteam] upload exceeds size plan"
CREATE2=$(curl -fsS -X POST "$COORD/api/market/orders" -H 'Content-Type: application/json' \
  -d "{\"label\":\"cap\",\"client_ref\":\"rt\",\"size_plan_bytes\":100,\"retention_days\":30,\"quote_hash\":\"$QH\"}")
OID2=$(echo "$CREATE2" | jq -r '.result.order.order_id')
TOK2=$(echo "$CREATE2" | jq -r '.result.upload_token')
BIG=$(python3 -c 'print("aa"*70)')
curl -fsS -X POST "$COORD/api/market/orders/$OID2/upload" \
  -H "Content-Type: application/json" -H "X-HMS-Upload-Token: $TOK2" \
  -d "{\"chunk_index\":0,\"ciphertext_hex\":\"$BIG\"}" | jq -e '.ok == true' >/dev/null
OVER=$(python3 -c 'print("bb"*50)')
curl -fsS -X POST "$COORD/api/market/orders/$OID2/upload" \
  -H "Content-Type: application/json" -H "X-HMS-Upload-Token: $TOK2" \
  -d "{\"chunk_index\":1,\"ciphertext_hex\":\"$OVER\"}" 2>/dev/null && exit 1 || true

echo "[hms-redteam] download without token"
curl -fsS -X POST "$COORD/api/market/orders/$OID/upload" \
  -H "Content-Type: application/json" -H "X-HMS-Upload-Token: $TOK" \
  -d "{\"chunk_index\":0,\"ciphertext_hex\":\"$HEX\"}" | jq -e '.ok == true' >/dev/null
curl -fsS -X POST "$COORD/api/market/orders/$OID/complete" -H "X-HMS-Upload-Token: $TOK" >/dev/null
curl -fsS "$COORD/api/market/orders/$OID/download/0" 2>/dev/null && exit 1 || true
curl -fsS "$COORD/api/market/orders/$OID/download/0?token=bad" 2>/dev/null && exit 1 || true

echo "[hms-redteam] valid download roundtrip"
curl -fsS "$COORD/api/market/orders/$OID/download/0?token=$TOK" -o "$TMP/got.bin"
cmp -s <(echo -n payload) "$TMP/got.bin"

echo "[hms-redteam] OK"
