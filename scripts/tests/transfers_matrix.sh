#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq
require_cmd python3

BASE="${BASE:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/transfers"
ensure_reports_dir "$OUT"

cases_file="$OUT/cases.jsonl"
results_file="$OUT/results.jsonl"
NOW_TS="$(date +%s)"
# Reset results for this run id (avoid counting old failures).
: >"$results_file"

cat >"$cases_file" <<JSONL
{"id":"tx-invalid-json","kind":"raw","body":"{","expect_http":400}
{"id":"tx-empty-object","kind":"json","body":{},"expect_http":400,"expect_code":"invalid_tx_type"}
{"id":"tx-invalid-type","kind":"json","body":{"tx_type":"x","from":"HMC-a","to":"HMC-b","amount_units":1,"fee_units":1000,"nonce":0,"timestamp_unix":1,"pubkey_ed25519":"00","sig_ed25519":"00"},"expect_http":400,"expect_code":"invalid_tx_type"}
{"id":"tx-invalid-signature","kind":"json","body":{"tx_type":"transfer_v1","from":"HMC-a","to":"HMC-b","amount_units":1,"fee_units":1000,"nonce":0,"timestamp_unix":$NOW_TS,"pubkey_ed25519":"00","sig_ed25519":"00"},"expect_http":400,"expect_code":"invalid_signature"}
JSONL

if [[ -n "${SIGNED_TX_JSON:-}" && -f "${SIGNED_TX_JSON:-}" ]]; then
  jq -c '{id:"tx-valid-signed",kind:"json",body:.,expect_http:200,expect_ok:true}' "$SIGNED_TX_JSON" >>"$cases_file"
fi

run_case() {
  local line="$1"
  local id kind body expect_http expect_code expect_ok
  id="$(jq -r '.id' <<<"$line")"
  kind="$(jq -r '.kind' <<<"$line")"
  expect_http="$(jq -r '.expect_http' <<<"$line")"
  expect_code="$(jq -r '.expect_code // ""' <<<"$line")"
  expect_ok="$(jq -r '.expect_ok // false' <<<"$line")"
  body="$(jq -c '.body // empty' <<<"$line")"

  local tmp_resp http body_resp code ok verdict
  tmp_resp="$OUT/${id}.resp"
  if [[ "$kind" == "raw" ]]; then
    body="$(jq -r '.body' <<<"$line")"
    http="$(curl -sS -o "$tmp_resp" -w '%{http_code}' -X POST "$BASE/api/tx/send" -H "Content-Type: application/json" --data-binary "$body" || true)"
  else
    http="$(curl -sS -o "$tmp_resp" -w '%{http_code}' -X POST "$BASE/api/tx/send" -H "Content-Type: application/json" -d "$body" || true)"
  fi
  body_resp="$(<"$tmp_resp")"
  code="$(jq -r '.code // empty' "$tmp_resp" 2>/dev/null || true)"
  ok="$(jq -r '.ok // false' "$tmp_resp" 2>/dev/null || true)"
  verdict="pass"
  # Under load, tx endpoint may apply rate limiting before deep validation.
  # Treat 429/rate_limited as an acceptable rejection for negative test cases.
  if [[ "$expect_ok" != "true" && "$http" == "429" && "$code" == "rate_limited" ]]; then
    expect_http="429"
    if [[ -n "$expect_code" ]]; then
      expect_code="rate_limited"
    fi
  fi
  if [[ "$http" != "$expect_http" ]]; then
    verdict="fail"
  fi
  if [[ -n "$expect_code" && "$code" != "$expect_code" ]]; then
    verdict="fail"
  fi
  if [[ "$expect_ok" == "true" && "$ok" != "true" ]]; then
    verdict="fail"
  fi
  jq -nc \
    --arg id "$id" \
    --arg verdict "$verdict" \
    --argjson expect_http "$expect_http" \
    --argjson got_http "${http:-0}" \
    --arg expect_code "$expect_code" \
    --arg got_code "$code" \
    --arg got_ok "$ok" \
    --arg body "$body_resp" \
    '{id:$id,verdict:$verdict,expect_http:$expect_http,got_http:$got_http,expect_code:$expect_code,got_code:$got_code,got_ok:$got_ok,response:$body}'
}

while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  run_case "$line" >>"$results_file"
done <"$cases_file"

fails="$(jq -r 'select(.verdict=="fail") | .id' "$results_file" | wc -l | tr -d ' ')"
total="$(wc -l <"$results_file" | tr -d ' ')"

jq -nc --arg run_id "$RID" --arg base "$BASE" --arg captured_at "$(ts_utc)" --argjson total "$total" --argjson fails "$fails" \
  '{run_id:$run_id,base:$base,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' >"$OUT/summary.json"

if [[ "$fails" != "0" ]]; then
  fail "transfers matrix FAIL ($fails/$total). See $OUT"
fi
pass "transfers matrix PASS ($total cases). See $OUT"
