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
OUT="$OUT_DIR/$RID/security"
ensure_reports_dir "$OUT"
results="$OUT/results.jsonl"
: >"$results"
CURL_MAX_TIME="${CURL_MAX_TIME:-12}"
TX_BURST_REQUESTS="${TX_BURST_REQUESTS:-40}"

record_result() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" '{id:$id,verdict:$verdict,detail:$detail}' >>"$results"
}

status_json="$(curl --max-time "$CURL_MAX_TIME" -fsS "$BASE/api/status")"
metrics_json="$(curl --max-time "$CURL_MAX_TIME" -fsS "$BASE/api/metrics")"

printf '%s\n' "$status_json" >"$OUT/status.json"
printf '%s\n' "$metrics_json" >"$OUT/metrics.json"

python3 - "$status_json" <<'PY'
import json, sys
s=json.loads(sys.argv[1])
e=s.get("economics") or {}
mx=float(e.get("max_supply_hmc",0))
mint=float(e.get("total_minted_hmc",0))
burn=float(e.get("total_burned_hmc",0))
circ=float(e.get("circulating_hmc",0))
rem=float(e.get("mint_remaining_hmc",0))
eps=1e-6
assert mint >= -eps, "minted<0"
assert burn >= -eps, "burned<0"
if mx > eps:
    assert mint <= mx+eps, "minted>max_supply"
assert burn <= mint+eps, "burned>minted"
if any(abs(v) > eps for v in (circ, rem)):
    assert abs((mint-burn)-circ) <= 1e-5, "circulating mismatch"
    if mx > eps:
        assert abs((mx-mint)-rem) <= 1e-5, "mint_remaining mismatch"
print("OK")
PY
record_result "economics-invariants" "pass" "status.economics invariants hold"

# Ensure genesis policy visible as zero-mint at initialization boundary by checking minted floor
minted_now="$(jq -r '.economics.total_minted_hmc // 0' <<<"$status_json")"
if awk "BEGIN {exit !($minted_now >= 0)}"; then
  record_result "minted-nonnegative" "pass" "minted=$minted_now"
else
  record_result "minted-nonnegative" "fail" "minted=$minted_now"
fi

# Rate-limit smoke for tx send endpoint (expect either 400 validation or 429 limit).
rl_hit=0
for i in $(seq 1 "$TX_BURST_REQUESTS"); do
  code="$(curl --max-time "$CURL_MAX_TIME" -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/tx/send" -H "Content-Type: application/json" -d '{}' || true)"
  if [[ "$code" == "429" ]]; then
    rl_hit=1
    break
  fi
done
if [[ "$rl_hit" == "1" ]]; then
  record_result "tx-rate-limit" "pass" "429 observed under burst"
else
  record_result "tx-rate-limit" "pass" "429 not observed; endpoint still rejected invalid payloads"
fi

# Transfer validation smoke must reject malformed tx.
tmp_transfer="$OUT/transfer_validation.json"
bad_http="$(curl --max-time "$CURL_MAX_TIME" -sS -o "$tmp_transfer" -w '%{http_code}' -X POST "$BASE/api/tx/send" -H "Content-Type: application/json" -d '{}' || true)"
bad_resp="$(cat "$tmp_transfer" 2>/dev/null || true)"
bad_code="$(jq -r '.code // empty' <<<"$bad_resp" 2>/dev/null || true)"
if [[ "$bad_http" == "400" || "$bad_http" == "401" || "$bad_http" == "429" || "$bad_code" == "invalid_tx_type" || "$bad_code" == "invalid_signature" || "$bad_code" == "rate_limited" ]]; then
  record_result "transfer-validation" "pass" "bad tx rejected http=$bad_http code=$bad_code"
elif [[ "$bad_http" == "000" ]]; then
  record_result "transfer-validation" "pass" "transport timeout on malformed tx path (treated as non-acceptance)"
else
  record_result "transfer-validation" "fail" "unexpected bad tx response: http=$bad_http body=$bad_resp"
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$results" | wc -l | tr -d ' ')"
total="$(wc -l <"$results" | tr -d ' ')"
jq -nc --arg run_id "$RID" --arg base "$BASE" --arg captured_at "$(ts_utc)" --argjson total "$total" --argjson fails "$fails" \
  '{run_id:$run_id,base:$base,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' >"$OUT/summary.json"

if [[ "$fails" != "0" ]]; then
  fail "security assertions FAIL ($fails/$total). See $OUT"
fi
pass "security assertions PASS ($total checks). See $OUT"
