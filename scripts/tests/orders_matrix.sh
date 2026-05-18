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
OUT="$OUT_DIR/$RID/orders"
ensure_reports_dir "$OUT"

ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-${TOKEN:-}}}"
TASK_NS="${TASK_NS:-${RID}-$(date +%s)}"

post_task() {
  local body="$1"
  local out_file="$2"
  curl -sS -o "$out_file" -w '%{http_code}' -X POST "$BASE/api/tasks" \
    -H "Content-Type: application/json" \
    ${ADMIN_TOKEN:+-H "X-Hackme-Admin-Token: $ADMIN_TOKEN"} \
    -d "$body"
}

results="$OUT/results.jsonl"
: >"$results"

run_case() {
  local id="$1" body="$2" expect_http="$3"
  local resp="$OUT/${id}.resp"
  local http attempts max_attempts
  attempts=0
  max_attempts=10
  while true; do
    attempts=$((attempts + 1))
    http="$(post_task "$body" "$resp" || true)"
    if [[ "$http" == "500" ]] || jq -e '((.error // "") | tostring | test("SQLITE_BUSY|database is locked"))' "$resp" >/dev/null 2>&1; then
      if (( attempts < max_attempts )); then
        sleep 0.6
        continue
      fi
    fi
    break
  done
  jq -nc --arg id "$id" --argjson expect_http "$expect_http" --argjson got_http "${http:-0}" --arg response "$(cat "$resp" 2>/dev/null || true)" \
    --arg verdict "$([[ "${http:-0}" == "$expect_http" ]] && echo pass || echo fail)" \
    '{id:$id,verdict:$verdict,expect_http:$expect_http,got_http:$got_http,response:$response}' >>"$results"
}

# Negative fairness guard
run_case "order-fairness-reject" \
  "{\"id\":\"order-fairness-reject-${TASK_NS}\",\"kind\":\"synthetic_poh_v1\",\"difficulty_score\":70,\"reward_hmc\":0.01,\"target_solves\":1,\"payer_ref\":\"qa:fairness\"}" \
  400

wallet="$(json_get "$BASE/api/wallet" || true)"
balance="$(
  printf '%s' "${wallet:-{}}" \
    | jq -er '.balance_hmc // 0 | tonumber' 2>/dev/null \
    || printf '0'
)"
balance="$(printf '%s' "$balance" | tr -d '\r\n')"

# Attempt valid tiny order when wallet is funded.
if python3 - "$balance" <<'PY'
import sys
try:
    bal = float(sys.argv[1].strip() or "0")
except Exception:
    bal = 0.0
raise SystemExit(0 if bal >= 0.02 else 1)
PY
then
  run_case "order-valid-small" \
    "{\"id\":\"order-valid-small-${TASK_NS}\",\"kind\":\"synthetic_poh_v1\",\"difficulty_score\":1,\"reward_hmc\":0.01,\"target_solves\":1,\"payer_ref\":\"qa:valid\"}" \
    200
else
  warn "wallet balance too low for positive order case (balance_hmc=$balance); skipping order-valid-small"
fi

# Always run insufficient funds expectation with intentionally large prepaid.
run_case "order-insufficient-funds" \
  "{\"id\":\"order-insufficient-funds-${TASK_NS}\",\"kind\":\"synthetic_poh_v1\",\"difficulty_score\":10,\"reward_hmc\":9999,\"target_solves\":9999,\"payer_ref\":\"qa:insufficient\"}" \
  402

tasks_json="$(json_get "$BASE/api/tasks" || true)"
printf '%s\n' "${tasks_json:-{}}" >"$OUT/tasks_snapshot.json"

fails="$(jq -r 'select(.verdict=="fail") | .id' "$results" | wc -l | tr -d ' ')"
total="$(wc -l <"$results" | tr -d ' ')"
jq -nc --arg run_id "$RID" --arg base "$BASE" --arg captured_at "$(ts_utc)" --argjson total "$total" --argjson fails "$fails" \
  '{run_id:$run_id,base:$base,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' >"$OUT/summary.json"

if [[ "$fails" != "0" ]]; then
  fail "orders matrix FAIL ($fails/$total). See $OUT"
fi
pass "orders matrix PASS ($total cases). See $OUT"
