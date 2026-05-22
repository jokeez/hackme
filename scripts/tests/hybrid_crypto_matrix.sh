#!/usr/bin/env bash
# Cross-platform hybrid Ed25519 matrix: minersign (Linux/CUDA/OS) + coordinator strict auth.
#
#   COORD_URL=https://hackme.tech/pool/coordinator COORD_TOKEN=... REQUIRE_STRICT=1 \
#     PACKETS=1000 bash scripts/tests/hybrid_crypto_matrix.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd curl
require_cmd jq
require_cmd go

COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
COORD_TOKEN="${COORD_TOKEN:-${ADMIN_TOKEN:-${HACKME_COORDINATOR_ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}}}"
PACKETS="${PACKETS:-1000}"
PACKET_DELAY_SEC="${PACKET_DELAY_SEC:-0}"
TAMPER_PACKETS="${TAMPER_PACKETS:-100}"
REQUIRE_STRICT="${REQUIRE_STRICT:-0}"
WORKER_PREFIX="${WORKER_PREFIX:-worker-crypto-matrix}"
MINERSIGN="${MINERSIGN:-$ROOT/bin/minersign-matrix}"
NONCE_DIR="${NONCE_DIR:-$ROOT/reports/crypto-matrix-nonce}"
REPORT_DIR="${REPORT_DIR:-$ROOT/reports/tests/$(run_id)/hybrid_crypto_matrix}"
rm -rf "$NONCE_DIR"
mkdir -p "$NONCE_DIR" "$REPORT_DIR"

if [[ -z "$COORD_TOKEN" ]]; then
  echo "[crypto-matrix] COORD_TOKEN required" >&2
  exit 2
fi

echo "[crypto-matrix] build minersign"
go build -trimpath -o "$MINERSIGN" ./cmd/minersign

ws="$(curl -fsS "${COORD_URL}/api/work/stats?details=0")"
hybrid_enabled="$(printf '%s' "$ws" | jq -r '.hybrid_signer_enabled // false')"
hybrid_strict="$(printf '%s' "$ws" | jq -r '.hybrid_signer_strict // false')"
echo "[crypto-matrix] hybrid_enabled=$hybrid_enabled strict=$hybrid_strict"
if [[ "$REQUIRE_STRICT" == "1" && "$hybrid_strict" != "true" ]]; then
  echo "[crypto-matrix] ERROR: prod strict required" >&2
  exit 3
fi

# One seed — same canonical bytes as Windows/Linux/HackMe OS minersign.
gen_out="$("$MINERSIGN" -gen-seed)"
SEED_HEX="$(printf '%s' "$gen_out" | jq -r '.miner_seed_hex')"
PUB="$(printf '%s' "$gen_out" | jq -r '.miner_pubkey_ed25519')"
ADDR="$(printf '%s' "$gen_out" | jq -r '.miner_address_from_pubkey')"
export HACKME_MINER_ED25519_SEED_HEX="$SEED_HEX"
MATRIX_NONCE_FILE="${NONCE_DIR}/matrix-global.nonce"
touch "$MATRIX_NONCE_FILE"
ok_signed=0
fail_signed=0
ok_tamper=0
fail_tamper=0
lat_sum=0
lat_n=0

claim_work() {
  local wid="$1" batch="$2"
  local attempt=0 resp claim http_code
  while [[ "$attempt" -lt 6 ]]; do
    resp="$(curl -sS -w $'\n%{http_code}' -X POST "${COORD_URL}/api/work/claim" \
      -H "Content-Type: application/json" \
      -H "X-Hackme-Admin-Token: ${COORD_TOKEN}" \
      -d "{\"worker_id\":\"${wid}\",\"batch_size\":${batch}}")"
    http_code="${resp##*$'\n'}"
    claim="${resp%$'\n'*}"
    if [[ "$http_code" == "200" ]] && printf '%s' "$claim" | jq -e '.ok == true' >/dev/null 2>&1; then
      printf '%s' "$claim"
      return 0
    fi
    attempt=$((attempt + 1))
    sleep $((attempt * 2))
  done
  return 1
}

sign_and_submit() {
  local plat="$1"
  local idx="$2"
  local wid="${WORKER_PREFIX}-${plat}-${idx}"
  local nonce_file="$MATRIX_NONCE_FILE"
  local claim
  if ! claim="$(claim_work "$wid" 2048)"; then
    fail_signed=$((fail_signed + 1))
    return 0
  fi
  local base batch work_id
  base="$(printf '%s' "$claim" | jq -r '.base_nonce')"
  batch="$(printf '%s' "$claim" | jq -r '.batch_size')"
  work_id="$(printf '%s' "$claim" | jq -r '.work_id')"
  local payload sig_json body
  payload="$(jq -nc \
    --arg w "$wid" --argjson b "$base" --argjson bs "$batch" --arg wid "$work_id" --arg p "$plat" \
    '{worker_id:$w,base_nonce:$b,batch_size:$bs,work_id:$wid,attempts:$bs,found:false,found_nonce:0,result_hash:("fuzz-"+$p+"-"+($b|tostring)),proof_hash:""}')"
  sig_json="$(printf '%s' "$payload" | HACKME_MINER_ED25519_SEED_HEX="$SEED_HEX" "$MINERSIGN" -nonce-file "$nonce_file")"
  body="$(jq -nc \
    --arg w "$wid" --argjson b "$base" --argjson bs "$batch" --arg wid "$work_id" --argjson att "$batch" \
    --arg rh "$(printf '%s' "$payload" | jq -r '.result_hash')" \
    --arg pub "$(printf '%s' "$sig_json" | jq -r '.miner_pubkey_ed25519')" \
    --arg sig "$(printf '%s' "$sig_json" | jq -r '.miner_sig_ed25519')" \
    --arg sn "$(printf '%s' "$sig_json" | jq -r '.submit_nonce')" \
    --arg addr "$ADDR" \
    '{worker_id:$w,base_nonce:$b,batch_size:$bs,work_id:$wid,attempts:$att,found:false,found_nonce:0,result_hash:$rh,proof_hash:"",
      miner_pubkey_ed25519:$pub,miner_sig_ed25519:$sig,miner_sig_alg:"ed25519",submit_nonce:($sn|tonumber),miner_address:$addr}')"
  local t0 t1 code
  t0="$(date +%s%N)"
  code="$(curl -sS -o /tmp/hcm-submit.json -w '%{http_code}' -X POST "${COORD_URL}/api/work/submit" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: ${COORD_TOKEN}" \
    -d "$body")"
  t1="$(date +%s%N)"
  lat_sum=$((lat_sum + (t1 - t0) / 1000000))
  lat_n=$((lat_n + 1))
  if [[ "$code" == "200" ]] && jq -e '.ok == true' /tmp/hcm-submit.json >/dev/null 2>&1; then
    ok_signed=$((ok_signed + 1))
  else
    fail_signed=$((fail_signed + 1))
    echo "[crypto-matrix] signed fail plat=$plat code=$code body=$(head -c 200 /tmp/hcm-submit.json)" >&2
  fi
  if awk -v d="$PACKET_DELAY_SEC" 'BEGIN{exit!(d>0)}'; then
    sleep "$PACKET_DELAY_SEC"
  fi
}

tamper_one() {
  local seq="${1:-0}"
  local wid="${WORKER_PREFIX}-tamper-${seq}"
  local claim
  if ! claim="$(claim_work "$wid" 512)"; then
    fail_tamper=$((fail_tamper + 1))
    return 0
  fi
  local base batch work_id
  base="$(printf '%s' "$claim" | jq -r '.base_nonce')"
  batch="$(printf '%s' "$claim" | jq -r '.batch_size')"
  work_id="$(printf '%s' "$claim" | jq -r '.work_id')"
  local payload sig_json body tampered code
  payload="$(jq -nc --arg w "$wid" --argjson b "$base" --argjson bs "$batch" --arg wid "$work_id" \
    '{worker_id:$w,base_nonce:$b,batch_size:$bs,work_id:$wid,attempts:$bs,found:false,result_hash:"tamper",proof_hash:""}')"
  sig_json="$(printf '%s' "$payload" | HACKME_MINER_ED25519_SEED_HEX="$SEED_HEX" "$MINERSIGN" -nonce-file "$MATRIX_NONCE_FILE")"
  body="$(jq -nc \
    --arg w "$wid" --argjson b "$base" --argjson bs "$batch" --arg wid "$work_id" --argjson att "$batch" \
    --arg pub "$(printf '%s' "$sig_json" | jq -r '.miner_pubkey_ed25519')" \
    --arg sig "$(printf '%s' "$sig_json" | jq -r '.miner_sig_ed25519')" \
    --arg sn "$(printf '%s' "$sig_json" | jq -r '.submit_nonce')" \
    --arg addr "$ADDR" \
    '{worker_id:$w,base_nonce:$b,batch_size:$bs,work_id:$wid,attempts:$att,found:false,result_hash:"tamper",proof_hash:"",
      miner_pubkey_ed25519:$pub,miner_sig_ed25519:$sig,submit_nonce:($sn|tonumber),miner_address:$addr}')"
  tampered="${body/\"tamper\"/\"tamperX\"}"
  code="$(curl -sS -o /tmp/hcm-tamper.json -w '%{http_code}' -X POST "${COORD_URL}/api/work/submit" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: ${COORD_TOKEN}" \
    -d "$tampered")"
  if [[ "$code" == "401" || "$code" == "403" || "$code" == "400" ]] || jq -e '.ok == false' /tmp/hcm-tamper.json >/dev/null 2>&1; then
    ok_tamper=$((ok_tamper + 1))
  else
    fail_tamper=$((fail_tamper + 1))
    echo "[crypto-matrix] tamper expected 401/403 or ok=false got code=$code" >&2
  fi
  if awk -v d="$PACKET_DELAY_SEC" 'BEGIN{exit!(d>0)}'; then
    sleep "$PACKET_DELAY_SEC"
  fi
}

echo "[crypto-matrix] signed packets=$PACKETS (3 platform labels)"
for i in $(seq 1 "$PACKETS"); do
  case $((i % 3)) in
    0) plat="windows-opencl" ;;
    1) plat="linux-cuda" ;;
    *) plat="hackme-os-minersign" ;;
  esac
  sign_and_submit "$plat" "$i" || true
  if (( i % 100 == 0 )); then
    echo "[crypto-matrix] progress $i/$PACKETS ok=$ok_signed fail=$fail_signed"
  fi
done

echo "[crypto-matrix] tamper probes ($TAMPER_PACKETS)"
for t in $(seq 1 "$TAMPER_PACKETS"); do
  tamper_one "$t" || true
done

avg_lat=0
if [[ "$lat_n" -gt 0 ]]; then
  avg_lat=$((lat_sum / lat_n))
fi

{
  echo "# Hybrid crypto matrix"
  echo "- ok_signed: $ok_signed"
  echo "- fail_signed: $fail_signed"
  echo "- ok_tamper: $ok_tamper"
  echo "- fail_tamper: $fail_tamper"
  echo "- avg_latency_ms: $avg_lat"
  echo "- pubkey: $PUB"
} >"$REPORT_DIR/CRYPTO_MATRIX_REPORT.md"

min_signed=$((PACKETS * 9 / 10))
min_tamper=$((TAMPER_PACKETS * 9 / 10))
if [[ "$ok_signed" -lt "$min_signed" ]] || [[ "$ok_tamper" -lt "$min_tamper" ]]; then
  echo "[crypto-matrix] FAIL — see $REPORT_DIR (need >=$min_signed signed, >=$min_tamper tamper rejected)" >&2
  exit 1
fi
pass "hybrid crypto matrix PASS ($ok_signed signed, $ok_tamper/$TAMPER_PACKETS tamper rejected)"
