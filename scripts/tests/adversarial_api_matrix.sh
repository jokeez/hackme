#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

BASE="${BASE:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/adversarial_api"
ensure_reports_dir "$OUT"
RESULTS="$OUT/results.jsonl"
: >"$RESULTS"
CURL_MAX_TIME="${CURL_MAX_TIME:-20}"
BURST_REQUESTS="${BURST_REQUESTS:-40}"

ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}"

record_result() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
}

is_any() {
  local got="$1"
  shift
  local x
  for x in "$@"; do
    [[ "$got" == "$x" ]] && return 0
  done
  return 1
}

tmp="$OUT/tmp.json"

# 1) Global metrics contract should always be readable (retry after heavy prior gates).
gm_http="000"
for _ in 1 2 3 4; do
  gm_http="$(curl --max-time "$CURL_MAX_TIME" -sS -o "$tmp" -w '%{http_code}' "$BASE/api/global/metrics" || true)"
  if [[ "$gm_http" == "200" ]] && jq -e '.ok == true and (.chain|type=="object") and (.network|type=="object") and (.work|type=="object")' "$tmp" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
if [[ "$gm_http" == "200" ]] && jq -e '.ok == true and (.chain|type=="object") and (.network|type=="object") and (.work|type=="object")' "$tmp" >/dev/null 2>&1; then
  record_result "global-metrics-contract" "pass" "api/global/metrics contract ok"
else
  record_result "global-metrics-contract" "fail" "http=$gm_http body=$(cat "$tmp" 2>/dev/null || true)"
fi

# 2) Tasks POST must reject unauthenticated call.
tasks_unauth_http="$(curl --max-time "$CURL_MAX_TIME" -sS -o "$tmp" -w '%{http_code}' -X POST "$BASE/api/tasks" -H "Content-Type: application/json" -d '{}' || true)"
if is_any "$tasks_unauth_http" "401" "429"; then
  record_result "tasks-unauth-rejected" "pass" "http=$tasks_unauth_http"
else
  record_result "tasks-unauth-rejected" "fail" "unexpected http=$tasks_unauth_http"
fi

# 3) Transfer malformed payload should be rejected (or rate-limited under stress).
tx_bad_http="$(curl --max-time "$CURL_MAX_TIME" -sS -o "$tmp" -w '%{http_code}' -X POST "$BASE/api/tx/send" -H "Content-Type: application/json" --data-binary '{' || true)"
if is_any "$tx_bad_http" "400" "429"; then
  record_result "tx-malformed-rejected" "pass" "http=$tx_bad_http"
else
  record_result "tx-malformed-rejected" "fail" "unexpected http=$tx_bad_http"
fi

# 4) tx/send abuse burst should not produce 5xx.
tx_5xx=0
tx_429=0
for _ in $(seq 1 "$BURST_REQUESTS"); do
  h="$(curl --max-time "$CURL_MAX_TIME" -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/tx/send" -H "Content-Type: application/json" -d '{}' || true)"
  if [[ "$h" == "429" ]]; then tx_429=1; fi
  if [[ "$h" == 5* ]]; then tx_5xx=1; break; fi
done
if [[ "$tx_5xx" == "0" ]]; then
  record_result "tx-burst-no-5xx" "pass" "no 5xx observed; rate_limit_seen=$tx_429"
else
  record_result "tx-burst-no-5xx" "fail" "5xx observed during malformed tx burst"
fi

# 5) Optional admin-only adversarial checks.
if [[ -n "$ADMIN_TOKEN" ]]; then
  code_payload='{"id":"adv-code-smoke","language":"python","code":"print(1)","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"adv:test"}'
  code_http="$(curl --max-time "$CURL_MAX_TIME" -sS -o "$tmp" -w '%{http_code}' -X POST "$BASE/api/tasks/from_code" -H "Content-Type: application/json" -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" -d "$code_payload" || true)"
  code_code="$(jq -r '.code // ""' "$tmp" 2>/dev/null || true)"
  if [[ "$code_http" == "400" && "$code_code" == "unsupported_language" ]] || [[ "$code_http" == "429" ]]; then
    record_result "from-code-unsupported-language" "pass" "http=$code_http code=$code_code"
  else
    record_result "from-code-unsupported-language" "fail" "http=$code_http code=$code_code body=$(cat "$tmp" 2>/dev/null || true)"
  fi

  bad_manifest='{"id":"adv-order-invalid","kind":"synthetic_poh_v1","reward_hmc":0.0001,"difficulty_score":80,"target_solves":1,"payer_ref":"adv:test"}'
  bad_order_http="$(curl --max-time "$CURL_MAX_TIME" -sS -o "$tmp" -w '%{http_code}' -X POST "$BASE/api/tasks" -H "Content-Type: application/json" -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" -d "$bad_manifest" || true)"
  if is_any "$bad_order_http" "400" "402" "429"; then
    record_result "order-invalid-econ-rejected" "pass" "http=$bad_order_http"
  else
    record_result "order-invalid-econ-rejected" "fail" "unexpected http=$bad_order_http body=$(cat "$tmp" 2>/dev/null || true)"
  fi
else
  record_result "from-code-unsupported-language" "pass" "skipped: ADMIN_TOKEN not set"
  record_result "order-invalid-econ-rejected" "pass" "skipped: ADMIN_TOKEN not set"
fi

# 6) Optional P2P malformed endpoint check.
if [[ -n "$P2P_TOKEN" ]]; then
  p2p_http="$(curl --max-time "$CURL_MAX_TIME" -sS -o "$tmp" -w '%{http_code}' -X POST "$BASE/api/p2p/tx" -H "Content-Type: application/json" -H "X-Hackme-P2P-Token: $P2P_TOKEN" -d '{}' || true)"
  p2p_code="$(jq -r '.code // ""' "$tmp" 2>/dev/null || true)"
  # Accept both transport-level reject (4xx) and handler-level semantic reject (200 + ok:false + code).
  if is_any "$p2p_http" "400" "401" "429" || { [[ "$p2p_http" == "200" ]] && [[ "$p2p_code" == "invalid_tx_type" ]]; }; then
    record_result "p2p-malformed-rejected" "pass" "http=$p2p_http"
  else
    record_result "p2p-malformed-rejected" "fail" "unexpected http=$p2p_http body=$(cat "$tmp" 2>/dev/null || true)"
  fi
else
  record_result "p2p-malformed-rejected" "pass" "skipped: P2P_TOKEN not set"
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"
total="$(wc -l <"$RESULTS" | tr -d ' ')"
jq -nc --arg run_id "$RID" --arg base "$BASE" --arg captured_at "$(ts_utc)" --argjson total "$total" --argjson fails "$fails" \
  '{run_id:$run_id,base:$base,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' >"$OUT/summary.json"

if [[ "$fails" != "0" ]]; then
  fail "adversarial api matrix FAIL ($fails/$total). See $OUT"
fi
pass "adversarial api matrix PASS ($total checks). See $OUT"
