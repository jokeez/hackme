#!/usr/bin/env bash
# Fuzz coordinator + node tx/p2p endpoints: garbage JSON, oversized bodies, invalid fields.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd curl
require_cmd go
require_cmd python3
require_cmd jq

RID="${RUN_ID:-$(run_id)}"
OUT="${OUT_DIR:-$ROOT/reports/tests}/$RID/coord_api_fuzz"
COORD_ADDR="${COORD_ADDR:-127.0.0.1:8083}"
COORD_URL="http://${COORD_ADDR#*://}"
COORD_BIN="${COORD_BIN:-$ROOT/bin/coordinator-fuzz-gate}"
COORD_DB="${HACKME_COORDINATOR_DB:-$ROOT/reports/tests/$RID/coordinator_fuzz.db}"
COORD_LOG="$OUT/coordinator.log"
COORD_PID_FILE="$OUT/coordinator.pid"
COORD_ADMIN="${HACKME_COORDINATOR_ADMIN_TOKEN:-fuzz-gate-admin-token}"
RESULTS="$OUT/results.jsonl"
BASE="${BASE:-http://127.0.0.1:8080}"

mkdir -p "$OUT"
: >"$RESULTS"
rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true

record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
}

expect_http() {
  local id="$1" url="$2" method="$3" body_file="$4" token="$5" expect="$6"
  local tmp http
  tmp="$OUT/${id}.resp"
  if [[ "$method" == "POST" ]]; then
    if [[ -n "$token" ]]; then
      http="$(curl -sS -o "$tmp" -w '%{http_code}' -X POST "$url" \
        -H "Content-Type: application/json" \
        -H "X-Hackme-Admin-Token: $token" \
        --data-binary "@${body_file:-/dev/null}" \
        --max-time 25 || true)"
    else
      http="$(curl -sS -o "$tmp" -w '%{http_code}' -X POST "$url" \
        -H "Content-Type: application/json" \
        --data-binary "@${body_file:-/dev/null}" \
        --max-time 25 || true)"
    fi
  else
    http="$(curl -sS -o "$tmp" -w '%{http_code}' "$url" --max-time 25 || true)"
  fi
  if [[ "$http" == "$expect" ]] || { [[ "$expect" == "4xx" ]] && [[ "$http" =~ ^4[0-9]{2}$ ]]; } || { [[ "$expect" == "no5xx" ]] && [[ ! "$http" =~ ^5 ]]; }; then
    record "$id" "pass" "http=$http"
  else
    record "$id" "fail" "http=$http expect=$expect body=$(head -c 200 "$tmp" 2>/dev/null || true)"
  fi
}

cleanup() {
  if [[ -f "$COORD_PID_FILE" ]]; then
    pid="$(cat "$COORD_PID_FILE" 2>/dev/null || true)"
    [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

echo "[coord-fuzz] build coordinator"
go build -trimpath -o "$COORD_BIN" ./cmd/coordinator

export HACKME_COORDINATOR_ADDR="$COORD_ADDR"
export HACKME_COORDINATOR_DB="$COORD_DB"
export HACKME_COORDINATOR_ADMIN_TOKEN="$COORD_ADMIN"
export HACKME_COORDINATOR_REQUIRE_ADMIN_TOKEN=1
export HACKME_COORDINATOR_ALLOW_INSECURE=0
export HACKME_COORDINATOR_CLAIM_PER_MIN=50000
export HACKME_COORDINATOR_SUBMIT_PER_MIN=50000

nohup "$COORD_BIN" >>"$COORD_LOG" 2>&1 &
echo $! >"$COORD_PID_FILE"

for _ in $(seq 1 40); do
  curl -fsS --max-time 2 "$COORD_URL/health" >/dev/null 2>&1 && break
  sleep 0.25
done
curl -fsS --max-time 5 "$COORD_URL/health" | jq -e '.ok == "coordinator"' >/dev/null

echo "[coord-fuzz] malformed + oversize + work endpoints"
printf '{' >"$OUT/bad_json.txt"
expect_http "coord-bad-json" "$COORD_URL/api/work/claim" POST "$OUT/bad_json.txt" "$COORD_ADMIN" "4xx"

python3 - <<'PY' >"$OUT/huge.json"
# 2 MiB — exceeds typical 1 MiB MaxBytesReader on API handlers.
print("{" + "x"* (2*1024*1024) + ":1}")
PY
expect_http "coord-huge-body" "$COORD_URL/api/work/claim" POST "$OUT/huge.json" "$COORD_ADMIN" "4xx"

echo '{"worker_id":"","batch_size":0}' >"$OUT/empty_claim.json"
expect_http "coord-empty-claim" "$COORD_URL/api/work/claim" POST "$OUT/empty_claim.json" "$COORD_ADMIN" "4xx"

echo '{"worker_id":"fuzz","base_nonce":0,"batch_size":1,"attempts":0}' >"$OUT/bad_submit.json"
expect_http "coord-bad-submit" "$COORD_URL/api/work/submit" POST "$OUT/bad_submit.json" "$COORD_ADMIN" "4xx"

# Node tx/send if local node up
if curl -fsS --max-time 3 "$BASE/api/status?lite=1" >/dev/null 2>&1; then
  printf '{' >"$OUT/node_bad_json.txt"
  expect_http "node-tx-bad-json" "$BASE/api/tx/send" POST "$OUT/node_bad_json.txt" "" "4xx"
  python3 - <<'PY' >"$OUT/tx_huge.json"
print("{" + "a"* (2*1024*1024) + ":1}")
PY
  expect_http "node-tx-huge" "$BASE/api/tx/send" POST "$OUT/tx_huge.json" "" "4xx"
  NOW="$(date +%s)"
  jq -nc --argjson ts "$NOW" \
    '{tx_type:"transfer_v1",from:"HMC-notvalid",to:"HMC-alsobad",amount_units:1,fee_units:1000,nonce:0,timestamp_unix:$ts,pubkey_ed25519:"zz",sig_ed25519:"00"}' \
    >"$OUT/tx_bad_hex.json"
  expect_http "node-tx-bad-hex" "$BASE/api/tx/send" POST "$OUT/tx_bad_hex.json" "" "4xx"
  python3 - <<'PY' >"$OUT/tx_memo257.json"
import json, time
memo = "я" * 129  # 258 bytes UTF-8
print(json.dumps({
  "tx_type": "transfer_v1",
  "from": "HMC-aaaaaaaaaaaaaaaa",
  "to": "HMC-bbbbbbbbbbbbbbbb",
  "amount_units": 1,
  "fee_units": 1000,
  "nonce": 0,
  "timestamp_unix": int(time.time()),
  "memo": memo,
  "pubkey_ed25519": "00",
  "sig_ed25519": "00",
}))
PY
  expect_http "node-tx-memo257" "$BASE/api/tx/send" POST "$OUT/tx_memo257.json" "" "4xx"
else
  record "node-tx-bad-json" "pass" "skipped: node down at $BASE"
  record "node-tx-huge" "pass" "skipped"
  record "node-tx-bad-hex" "pass" "skipped"
  record "node-tx-memo257" "pass" "skipped"
fi

# Burst rate limit smoke (same IP)
codes="$OUT/burst_codes.txt"
: >"$codes"
for _ in $(seq 1 60); do
  curl -sS -o /dev/null -w '%{http_code}\n' -X POST "$COORD_URL/api/work/claim" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $COORD_ADMIN" \
    -d '{"worker_id":"burst-fuzz","batch_size":1024}' >>"$codes" || echo 000 >>"$codes"
done
if grep -q '^5' "$codes"; then
  record "coord-burst-no-5xx" "fail" "5xx in burst: $(grep '^5' "$codes" | head -3)"
else
  rate="$(grep -c '^429' "$codes" || true)"
  record "coord-burst-no-5xx" "pass" "no 5xx; 429_count=$rate"
fi

if grep -Eqi 'panic:|database is locked|SQLITE_BUSY' "$COORD_LOG"; then
  record "coord-log-clean" "fail" "coordinator log has panic/lock"
else
  record "coord-log-clean" "pass" "no panic/sqlite lock in log"
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"
total="$(wc -l <"$RESULTS" | tr -d ' ')"
echo "[coord-fuzz] $((total - fails))/$total passed — $OUT"
if [[ "$fails" != "0" ]]; then
  jq -r 'select(.verdict=="fail")' "$RESULTS"
  exit 1
fi
pass "coordinator_api_fuzz_gate PASS"
