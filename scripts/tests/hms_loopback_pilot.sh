#!/usr/bin/env bash
# HMS loopback pilot: ephemeral coordinator + storage register/chunk + signed seal + payouts + settle dry-run.
# Uses short epochs and an easy seal target (local only — NOT production difficulty).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
TMP="$(mktemp -d)"
trap 'kill $CPID $SPID 2>/dev/null; rm -rf "$TMP"' EXIT

export HMS_COORDINATOR_ADDR="127.0.0.1:18096"
export HMS_COORDINATOR_DB="$TMP/hms.db"
export HMS_COORDINATOR_ALLOW_INSECURE=1
export HMS_EPOCH_SECONDS=40
export HMS_FREEZE_AFTER_SEC=8
export HMS_SEAL_WINDOW_SEC=28
export HMS_MIN_QUOTA_GB=1
export HMS_MARKET_STORAGE_ROOT="$TMP/storage"
export HMS_MARKET_DATA_DIR="$TMP/market"
export HMS_MARKET_SKIP_PAYMENT=1
export HMS_MARKET_REPLICAS=1
# Easy target: any non-max hash meets (local pilot only).
export HMS_INITIAL_SEAL_TARGET="fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe"

COORD="http://127.0.0.1:18096"
echo "[hms-loopback] build"
go build -trimpath -o "$TMP/hmscoordinator" ./cmd/hmscoordinator
go build -trimpath -o "$TMP/workerseal" ./cmd/workerseal
go build -trimpath -o "$TMP/workerstorage" ./cmd/workerstorage

mkdir -p "$TMP/storage/w-pilot"
"$TMP/hmscoordinator" >>"$TMP/coord.log" 2>&1 &
CPID=$!
for _ in $(seq 1 60); do
  curl -fsS --max-time 1 "$COORD/health" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS "$COORD/health" >/dev/null

PUB=$(printf '%64s' | tr ' ' 'a')
curl -fsS -X POST "$COORD/api/storage/register" -H 'Content-Type: application/json' \
  -d "{\"worker_id\":\"w-pilot\",\"pubkey_hex\":\"$PUB\",\"quota_gb\":50,\"storage_tier\":\"ssd\",\"host_label\":\"pilot-host\"}" >/dev/null

# Seed a PoSt chunk so manifest is non-empty after freeze.
HEX=$(python3 -c 'print("ab"*2048)')
SUM=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(bytes.fromhex(sys.argv[1])).hexdigest())' "$HEX")
curl -fsS -X POST "$COORD/api/storage/chunk" -H 'Content-Type: application/json' \
  -d "{\"chunk_id\":\"chunk-pilot-1\",\"worker_id\":\"w-pilot\",\"ciphertext_sha256\":\"$SUM\",\"size\":2048}" >/dev/null \
  || curl -fsS -X POST "$COORD/api/storage/chunk" -H 'Content-Type: application/json' \
  -d "{\"chunk_id\":\"chunk-pilot-1\",\"worker_id\":\"w-pilot\",\"ciphertext_sha256\":\"$SUM\",\"size_bytes\":2048}" >/dev/null

# Market upload/download roundtrip (restore path on coordinator local disk).
QUOTE=$(curl -fsS -X POST "$COORD/api/market/quote" -H 'Content-Type: application/json' \
  -d '{"size_bytes":32,"retention_days":30}')
QH=$(echo "$QUOTE" | jq -r '.quote.quote_hash')
CREATE=$(curl -fsS -X POST "$COORD/api/market/orders" -H 'Content-Type: application/json' \
  -d "{\"label\":\"pilot\",\"client_ref\":\"lb\",\"size_plan_bytes\":32,\"retention_days\":30,\"quote_hash\":\"$QH\"}")
OID=$(echo "$CREATE" | jq -r '.result.order.order_id')
TOK=$(echo "$CREATE" | jq -r '.result.upload_token')
PAYLOAD_HEX=$(echo -n 'loopback-restore-pilot-payload!!' | xxd -p -c 256)
curl -fsS -X POST "$COORD/api/market/orders/$OID/upload" \
  -H "Content-Type: application/json" -H "X-HMS-Upload-Token: $TOK" \
  -d "{\"chunk_index\":0,\"ciphertext_hex\":\"$PAYLOAD_HEX\"}" | jq -e '.ok == true' >/dev/null
curl -fsS -X POST "$COORD/api/market/orders/$OID/complete" -H "X-HMS-Upload-Token: $TOK" >/dev/null
curl -fsS "$COORD/api/market/orders/$OID/download/0?token=$TOK" -o "$TMP/restored.bin"
cmp -s <(echo -n 'loopback-restore-pilot-payload!!') "$TMP/restored.bin"
echo "[hms-loopback] market restore OK"

echo "[hms-loopback] waiting for seal window…"
SEALED=0
for _ in $(seq 1 90); do
  stats="$(curl -fsS "$COORD/api/pool/stats")"
  ep="$(echo "$stats" | jq -r '.current_epoch')"
  # Force freeze build by polling seal/work after freeze_after.
  if work="$(curl -fsS --max-time 2 "$COORD/api/seal/work" 2>/dev/null)"; then
    echo "[hms-loopback] seal work open epoch=$ep"
    break
  fi
  sleep 0.5
done

HACKME_HMS_COORDINATOR_URL="$COORD" HACKME_WORKER_ID="seal-pilot" \
  "$TMP/workerseal" >>"$TMP/seal.log" 2>&1 &
SPID=$!

for _ in $(seq 1 120); do
  stats="$(curl -fsS "$COORD/api/pool/stats")"
  if [[ "$(echo "$stats" | jq -r '.epoch_sealed')" == "true" ]]; then
    SEALED=1
    EPOCH="$(echo "$stats" | jq -r '.last_seal_epoch // .current_epoch')"
    break
  fi
  # Also check epochs API for any sealed row.
  if curl -fsS "$COORD/api/seal/epochs" | jq -e '.epochs | length > 0' >/dev/null 2>&1; then
    SEALED=1
    EPOCH="$(curl -fsS "$COORD/api/seal/epochs" | jq -r '.epochs[0].epoch_id')"
    break
  fi
  sleep 0.5
done
kill "$SPID" 2>/dev/null || true
SPID=""

if [[ "$SEALED" != "1" ]]; then
  echo "[hms-loopback] FAIL: epoch not sealed" >&2
  tail -40 "$TMP/coord.log" "$TMP/seal.log" >&2 || true
  exit 1
fi
echo "[hms-loopback] sealed epoch=$EPOCH"

SETTLE=$(curl -fsS "$COORD/api/seal/payouts?epoch_id=$EPOCH")
echo "$SETTLE" | jq -e '.settlement.payouts_finalized == true' >/dev/null
echo "$SETTLE" | jq -e '(.settlement.payouts | length) >= 1' >/dev/null
POLICY=$(echo "$SETTLE" | jq -r '.settlement.policy_version')
[[ "$POLICY" == "hms-lane-v2-base-0.5" ]]

export HMS_COORD_URL="$COORD"
export DRY_RUN=1
export STATE_FILE="$TMP/settle_state.json"
export WORKER_PAYOUT_MAP="seal-pilot=HMC-pilot-test-addr,w-pilot=HMC-storage-test-addr"
bash "$ROOT/scripts/ops/settle_worker_hms.sh" | tee "$TMP/settle.out"
grep -q 'DRY_RUN' "$TMP/settle.out"

echo "[hms-loopback] OK epoch=$EPOCH policy=$POLICY"
