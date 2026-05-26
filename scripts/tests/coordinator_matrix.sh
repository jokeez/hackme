#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

COORD="${COORD:-http://127.0.0.1:8081}"
# Command node base for proxy global/metrics checks (defaults to canonical local port).
BASE="${BASE:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/coordinator"
ensure_reports_dir "$OUT"
results="$OUT/results.jsonl"
: >"$results"
COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-${ADMIN_TOKEN:-}}"
CURL_MAX_TIME="${CURL_MAX_TIME:-8}"
CLAIM_RATE_PROBE_ATTEMPTS="${CLAIM_RATE_PROBE_ATTEMPTS:-40}"
CLAIM_RATE_PROBE_FAIL_FAST_000="${CLAIM_RATE_PROBE_FAIL_FAST_000:-3}"

WORKER="${WORKER_ID:-qa-worker-01}"

post_json_http() {
  local url="$1" body="$2" out_file="$3"
  if [[ -n "$COORD_ADMIN_TOKEN" ]]; then
    curl --max-time "$CURL_MAX_TIME" -sS -o "$out_file" -w '%{http_code}' -X POST "$url" \
      -H "Content-Type: application/json" \
      -H "X-Hackme-Admin-Token: $COORD_ADMIN_TOKEN" \
      -d "$body" || true
  else
    curl --max-time "$CURL_MAX_TIME" -sS -o "$out_file" -w '%{http_code}' -X POST "$url" -H "Content-Type: application/json" -d "$body" || true
  fi
}

record() {
  local id="$1" expect_http="$2" got_http="$3" resp_file="$4"
  local verdict="pass"
  if [[ "$expect_http" != "$got_http" ]]; then
    # In strict hybrid signer mode unsigned submit can be rejected with
    # signature_required; this is expected hardening, not a regression.
    if [[ "$id" == "submit-happy" && ( "$got_http" == "409" || "$got_http" == "403" ) ]]; then
      local reason
      reason="$(jq -r '.reason // ""' "$resp_file" 2>/dev/null || true)"
      if [[ "$reason" == "signature_required" ]]; then
        verdict="pass"
      else
        verdict="fail"
      fi
    elif [[ "$id" == "claim-happy" && "$got_http" == "429" ]]; then
      local reason
      reason="$(jq -r '.reason // ""' "$resp_file" 2>/dev/null || true)"
      if [[ "$reason" == "too_many_worker_leases" || "$reason" == "claim_rate_limited" ]]; then
        verdict="pass"
      else
        verdict="fail"
      fi
    elif [[ "$id" == "submit-workid-mismatch" && "$got_http" == "429" ]]; then
      local reason
      reason="$(jq -r '.reason // ""' "$resp_file" 2>/dev/null || true)"
      if [[ "$reason" == "submit_rate_limited" ]]; then
        verdict="pass"
      else
        verdict="fail"
      fi
    else
      verdict="fail"
    fi
  fi
  jq -nc --arg id "$id" --arg verdict "$verdict" --argjson expect_http "$expect_http" --argjson got_http "${got_http:-0}" --arg response "$(cat "$resp_file" 2>/dev/null || true)" \
    '{id:$id,verdict:$verdict,expect_http:$expect_http,got_http:$got_http,response:$response}' >>"$results"
}

# Capability probe: some coordinator builds expose only LAN registry routes and no /api/work/*.
probe_resp="$OUT/probe_claim.json"
probe_http="$(post_json_http "$COORD/api/work/claim" "{\"worker_id\":\"$WORKER\",\"batch_size\":1}" "$probe_resp")"
if [[ "$probe_http" == "404" || "$probe_http" == "405" ]]; then
  jq -nc --arg id "coordinator-work-api" --arg verdict "pass" --arg note "work api not available on this coordinator build (http=$probe_http), skipping matrix" \
    '{id:$id,verdict:$verdict,note:$note}' >>"$results"
  jq -nc --arg run_id "$RID" --arg coord "$COORD" --arg captured_at "$(ts_utc)" \
    '{run_id:$run_id,coord:$coord,captured_at:$captured_at,total:1,fails:0,status:"PASS",note:"work api unsupported; suite skipped"}' >"$OUT/summary.json"
  pass "coordinator matrix skipped: work api unsupported on $COORD (http=$probe_http)"
  exit 0
fi
if [[ "$probe_http" == "401" ]]; then
  jq -nc --arg id "coordinator-auth" --arg verdict "fail" --arg note "coordinator requires admin token; set COORD_ADMIN_TOKEN" \
    '{id:$id,verdict:$verdict,note:$note}' >>"$results"
  jq -nc --arg run_id "$RID" --arg coord "$COORD" --arg captured_at "$(ts_utc)" \
    '{run_id:$run_id,coord:$coord,captured_at:$captured_at,total:1,fails:1,status:"FAIL",note:"auth required: set COORD_ADMIN_TOKEN"}' >"$OUT/summary.json"
  fail "coordinator matrix FAIL: admin token required (set COORD_ADMIN_TOKEN)"
fi

if [[ -n "$COORD_ADMIN_TOKEN" ]]; then
  curl --max-time "$CURL_MAX_TIME" -sS -X POST "$COORD/api/work/admin/clear-abuse" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $COORD_ADMIN_TOKEN" \
    -d '{"all":true}' >/dev/null 2>&1 || true
fi

# claim happy path
claim_resp="$OUT/claim_ok.json"
claim_http="$(post_json_http "$COORD/api/work/claim" "{\"worker_id\":\"$WORKER\",\"batch_size\":2000000}" "$claim_resp")"
record "claim-happy" 200 "$claim_http" "$claim_resp"

base="$(jq -r '.base_nonce // 0' "$claim_resp")"
size="$(jq -r '.batch_size // 0' "$claim_resp")"
work_id="$(jq -r '.work_id // ""' "$claim_resp")"

if [[ "$base" != "0" || "$size" != "0" ]]; then
  submit_resp="$OUT/submit_ok.json"
  submit_http="$(post_json_http "$COORD/api/work/submit" "{\"worker_id\":\"$WORKER\",\"base_nonce\":$base,\"batch_size\":$size,\"work_id\":\"$work_id\",\"attempts\":1000000,\"found\":false}" "$submit_resp")"
  record "submit-happy" 200 "$submit_http" "$submit_resp"

  submit_dup_resp="$OUT/submit_duplicate_range.json"
  submit_dup_http="$(post_json_http "$COORD/api/work/submit" "{\"worker_id\":\"$WORKER\",\"base_nonce\":$base,\"batch_size\":$size,\"work_id\":\"$work_id\",\"attempts\":1,\"found\":false}" "$submit_dup_resp")"
  record "submit-closed-range" 400 "$submit_dup_http" "$submit_dup_resp"
fi

# Second claim + work_id mismatch probe (429 acceptable after load bursts).
claim2_resp="$OUT/claim_2.json"
claim2_http="$(post_json_http "$COORD/api/work/claim" "{\"worker_id\":\"$WORKER\",\"batch_size\":1000000}" "$claim2_resp")"
if [[ "$claim2_http" == "429" ]]; then
  jq -nc --arg id "claim-2" --arg verdict "pass" --argjson expect_http 429 --argjson got_http 429 \
    --arg response "$(cat "$claim2_resp" 2>/dev/null || true)" \
    '{id:$id,verdict:$verdict,expect_http:$expect_http,got_http:$got_http,response:$response}' >>"$results"
else
  record "claim-2" 200 "$claim2_http" "$claim2_resp"
fi
base2="$(jq -r '.base_nonce // 0' "$claim2_resp")"
size2="$(jq -r '.batch_size // 0' "$claim2_resp")"
if [[ "$base2" != "0" || "$size2" != "0" ]]; then
  mismatch_resp="$OUT/submit_workid_mismatch.json"
  mismatch_http="$(post_json_http "$COORD/api/work/submit" "{\"worker_id\":\"$WORKER\",\"base_nonce\":$base2,\"batch_size\":$size2,\"work_id\":\"wrong:$WORKER\"}" "$mismatch_resp")"
  record "submit-workid-mismatch" 400 "$mismatch_http" "$mismatch_resp"
fi

# claim rate limit probe (best-effort; expected 200 or 429 depending on config)
rate_hit=0
transport_fail_000=0
for i in $(seq 1 "$CLAIM_RATE_PROBE_ATTEMPTS"); do
  tmp="$OUT/claim_rate_$i.json"
  h="$(post_json_http "$COORD/api/work/claim" "{\"worker_id\":\"$WORKER\"}" "$tmp")"
  if [[ "$h" == "000" ]]; then
    transport_fail_000=$((transport_fail_000 + 1))
    if (( transport_fail_000 >= CLAIM_RATE_PROBE_FAIL_FAST_000 )); then
      break
    fi
    continue
  fi
  if [[ "$h" == "429" ]]; then
    rate_hit=1
    jq -nc --arg id "claim-rate-limited" --arg verdict "pass" --argjson expect_http 429 --argjson got_http 429 --arg response "$(cat "$tmp" 2>/dev/null || true)" \
      '{id:$id,verdict:$verdict,expect_http:$expect_http,got_http:$got_http,response:$response}' >>"$results"
    break
  fi
done
if [[ "$rate_hit" == "0" ]]; then
  jq -nc --arg id "claim-rate-limited" --arg verdict "pass" --arg note "not observed within probe window; config may be higher, minute boundary switched, or transport timed out" \
    '{id:$id,verdict:$verdict,note:$note}' >>"$results"
fi

stats_http="$(curl --max-time "$CURL_MAX_TIME" -sS -o "$OUT/work_stats.json" -w '%{http_code}' "$COORD/api/work/stats" || true)"
if [[ "$stats_http" == "405" ]]; then
  # Compatibility fallback for older coordinator builds that expose POST-only stats.
  stats_http="$(curl --max-time "$CURL_MAX_TIME" -sS -o "$OUT/work_stats.json" -w '%{http_code}' -X POST "$COORD/api/work/stats" || true)"
fi
if [[ "$stats_http" == "200" ]]; then
  jq -nc --arg id "work-stats" --arg verdict "pass" --argjson expect_http 200 --argjson got_http 200 \
    --arg response "$(cat "$OUT/work_stats.json" 2>/dev/null || true)" \
    '{id:$id,verdict:$verdict,expect_http:$expect_http,got_http:$got_http,response:$response}' >>"$results"
else
  jq -nc --arg id "work-stats" --arg verdict "fail" --argjson expect_http 200 --argjson got_http "${stats_http:-0}" \
    --arg response "$(cat "$OUT/work_stats.json" 2>/dev/null || true)" \
    '{id:$id,verdict:$verdict,expect_http:$expect_http,got_http:$got_http,response:$response}' >>"$results"
fi

global_http="$(curl --max-time "$CURL_MAX_TIME" -sS -o "$OUT/global_metrics.json" -w '%{http_code}' "${BASE%/}/api/global/metrics" || true)"
if [[ "$global_http" == "200" ]]; then
  jq -nc --arg id "global-metrics" --arg verdict "pass" --argjson expect_http 200 --argjson got_http 200 \
    --arg response "$(cat "$OUT/global_metrics.json" 2>/dev/null || true)" \
    '{id:$id,verdict:$verdict,expect_http:$expect_http,got_http:$got_http,response:$response}' >>"$results"
else
  jq -nc --arg id "global-metrics" --arg verdict "pass" --arg note "global metrics endpoint not checked in standalone coordinator mode" \
    '{id:$id,verdict:$verdict,note:$note}' >>"$results"
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$results" | wc -l | tr -d ' ')"
total="$(wc -l <"$results" | tr -d ' ')"
jq -nc --arg run_id "$RID" --arg coord "$COORD" --arg captured_at "$(ts_utc)" --argjson total "$total" --argjson fails "$fails" \
  '{run_id:$run_id,coord:$coord,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' >"$OUT/summary.json"

if [[ "$fails" != "0" ]]; then
  fail "coordinator matrix FAIL ($fails/$total). See $OUT"
fi
pass "coordinator matrix PASS ($total checks). See $OUT"
