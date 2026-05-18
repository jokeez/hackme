#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq
require_cmd python3

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/language_chaos_security"
ensure_reports_dir "$OUT"
RESULTS="$OUT/results.jsonl"
: >"$RESULTS"
TMP="$OUT/tmp.json"

if [[ -z "$ADMIN_TOKEN" ]]; then
  fail "language_chaos_security: ADMIN_TOKEN/HACKME_ADMIN_TOKEN is required"
fi

record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
}

post_json_file() {
  local payload_file="$1"
  curl -sS -o "$TMP" -w '%{http_code}' -X POST "$BASE/api/tasks/from_code" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
    --data-binary "@${payload_file}" || true
}

run_case_file() {
  local id="$1"
  local payload_file="$2"
  local expected="$3"
  local attempts max_attempts http code
  attempts=0
  max_attempts=8
  while true; do
    attempts=$((attempts + 1))
    http="$(post_json_file "$payload_file")"
    if [[ "$http" == "500" ]] || jq -e '((.error // "") | tostring | test("SQLITE_BUSY|database is locked"))' "$TMP" >/dev/null 2>&1; then
      if (( attempts < max_attempts )); then
        sleep 0.6
        continue
      fi
    fi
    break
  done
  code="$(jq -r '.code // ""' "$TMP" 2>/dev/null || true)"
  if [[ "${http}:${code}" =~ $expected ]]; then
    record "$id" "pass" "http=$http code=$code"
  else
    record "$id" "fail" "unexpected http=$http code=$code body=$(cat "$TMP" 2>/dev/null || true)"
  fi
}

mk_payload() {
  local path="$1" id="$2" lang="$3" code="$4" payer="$5"
  python3 - "$path" "$id" "$lang" "$code" "$payer" <<'PY'
import json, sys
path, task_id, lang, code, payer = sys.argv[1:]
payload = {
    "id": task_id,
    "language": lang,
    "code": code,
    "reward_hmc": 0.01,
    "difficulty_score": 1,
    "target_solves": 1,
    "payer_ref": payer,
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(payload, f)
PY
}

ts="$(date -u +%Y%m%dt%H%M%S)"

p_rs="$OUT/p_rs.json"
mk_payload "$p_rs" "chaos-rs-${ts}" "rs" '#[no_mangle] pub extern "C" fn check(n:i64)->i32{ if n%23==0 {1} else {0} }' "chaos:rs"
run_case_file "alias-rs" "$p_rs" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed)$'

p_cxx="$OUT/p_cxx.json"
mk_payload "$p_cxx" "chaos-cxx-${ts}" "c++" '#include <stdint.h>
extern "C" int32_t check(int64_t n){ return (n%23==0)?1:0; }' "chaos:cxx"
run_case_file "alias-cpp-plus" "$p_cxx" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed)$'

p_gcc="$OUT/p_gcc.json"
mk_payload "$p_gcc" "chaos-gcc-${ts}" "gcc" '#include <stdint.h>
int32_t check(int64_t n){ return (n%23==0)?1:0; }' "chaos:gcc"
run_case_file "alias-gcc-c" "$p_gcc" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed)$'

p_as="$OUT/p_as.json"
mk_payload "$p_as" "chaos-as-${ts}" "as" 'export function check(n: i64): i32 {
  return i32((n % 23) == 0 ? 1 : 0);
}' "chaos:as"
run_case_file "alias-as-assemblyscript" "$p_as" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed|400:wasm_sanitize_failed)$'

p_go="$OUT/p_go.json"
mk_payload "$p_go" "chaos-go-${ts}" "go" 'package main
//export check
func check(n int64) int32 { if n%23==0 { return 1 }; return 0 }
func main() {}' "chaos:go"
run_case_file "alias-go-tinygo" "$p_go" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed|400:wasm_sanitize_failed)$'

p_wast="$OUT/p_wast.json"
mk_payload "$p_wast" "chaos-wast-${ts}" "wast" '(module
  (func (export "check") (param i64) (result i32)
    local.get 0
    i64.const 23
    i64.rem_s
    i64.eqz
    if (result i32) i32.const 1 else i32.const 0 end))' "chaos:wast"
run_case_file "alias-wast-wat" "$p_wast" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed)$'

# Random unknown language flood should fail closed (unsupported/rate-limit), not panic.
for i in 1 2 3 4 5; do
  p_bad="$OUT/p_bad_$i.json"
  mk_payload "$p_bad" "chaos-bad-${ts}-${i}" "lang_${RANDOM}_$i" 'print("noop")' "chaos:bad"
  run_case_file "unknown-lang-$i" "$p_bad" '^(400:unsupported_language|429:rate_limited)$'
done

fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"
total="$(wc -l <"$RESULTS" | tr -d ' ')"
jq -nc \
  --arg run_id "$RID" \
  --arg base "$BASE" \
  --arg captured_at "$(ts_utc)" \
  --argjson total "$total" \
  --argjson fails "$fails" \
  '{run_id:$run_id,base:$base,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' \
  >"$OUT/summary.json"

if [[ "$fails" != "0" ]]; then
  fail "language chaos security FAIL ($fails/$total). See $OUT"
fi
pass "language chaos security PASS ($total checks). See $OUT"
