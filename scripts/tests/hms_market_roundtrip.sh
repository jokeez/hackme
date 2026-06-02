#!/usr/bin/env bash
# End-to-end: quote → create → upload → complete → download → verify bytes.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
COORD="${HACKME_HMS_COORDINATOR_URL:-http://127.0.0.1:18082}"
NODE="${BASE:-http://127.0.0.1:8080}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

PAYLOAD="$TMP/secret-payload.txt"
echo "HackMe HMS market roundtrip test $(date -u)" >"$PAYLOAD"
echo "Extra line for chunk content." >>"$PAYLOAD"
SIZE=$(wc -c <"$PAYLOAD")

echo "[roundtrip] payload $SIZE bytes"
echo "[roundtrip] quote"
QUOTE=$(curl -fsS -X POST "$COORD/api/market/quote" -H 'Content-Type: application/json' \
  -d "{\"size_bytes\":$SIZE,\"retention_days\":30}")
echo "$QUOTE" | jq '{total: .quote.total_debit_hmc, hash: .quote.quote_hash}'
QH=$(echo "$QUOTE" | jq -r '.quote.quote_hash')

echo "[roundtrip] create order"
CREATE=$(curl -fsS -X POST "$COORD/api/market/orders" -H 'Content-Type: application/json' \
  -d "{\"label\":\"roundtrip-test\",\"client_ref\":\"cli\",\"size_plan_bytes\":$SIZE,\"retention_days\":30,\"quote_hash\":\"$QH\"}")
echo "$CREATE" | jq '{order: .result.order.order_id, token_len: (.result.upload_token|length)}'
OID=$(echo "$CREATE" | jq -r '.result.order.order_id')
TOK=$(echo "$CREATE" | jq -r '.result.upload_token')

# Simulate client-side encryption (raw bytes for pilot — real UI uses AES-GCM)
CT=$(cat "$PAYLOAD")
HEX=$(xxd -p -c 1000000 "$PAYLOAD" | tr -d '\n')

echo "[roundtrip] upload chunk 0"
UP=$(curl -fsS -X POST "$COORD/api/market/orders/$OID/upload" \
  -H "Content-Type: application/json" -H "X-HMS-Upload-Token: $TOK" \
  -d "{\"chunk_index\":0,\"ciphertext_hex\":\"$HEX\"}")
echo "$UP" | jq '{ok, chunk_id, worker_id, size}'

echo "[roundtrip] complete"
curl -fsS -X POST "$COORD/api/market/orders/$OID/complete" -H "X-HMS-Upload-Token: $TOK" | jq .

echo "[roundtrip] list chunks"
curl -fsS "$COORD/api/market/orders/$OID/chunks" | jq .

echo "[roundtrip] download chunk 0"
curl -fsS "$COORD/api/market/orders/$OID/download/0?token=$TOK" -o "$TMP/downloaded.bin"
DL=$(wc -c <"$TMP/downloaded.bin")

if cmp -s "$PAYLOAD" "$TMP/downloaded.bin"; then
  echo "[roundtrip] OK — upload/download bytes match ($DL bytes)"
else
  echo "[roundtrip] FAIL — mismatch payload=$SIZE downloaded=$DL" >&2
  diff -u "$PAYLOAD" "$TMP/downloaded.bin" || true
  exit 1
fi

echo "[roundtrip] node pricing API"
curl -fsS "$NODE/api/hms/market/pricing" | jq -e '.pricing.policy_hash != null' >/dev/null
echo "[roundtrip] all passed — open http://127.0.0.1:8080 → HMS → Market for UI test"
