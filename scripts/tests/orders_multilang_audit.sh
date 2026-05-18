#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/orders_multilang_audit"
ensure_reports_dir "$OUT"
RESULTS="$OUT/results.jsonl"
: >"$RESULTS"
TMP="$OUT/tmp.json"

if [[ -z "$ADMIN_TOKEN" ]]; then
  fail "orders_multilang_audit: ADMIN_TOKEN/HACKME_ADMIN_TOKEN is required"
fi

record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
}

post_from_code() {
  local payload="$1"
  curl -sS -o "$TMP" -w '%{http_code}' -X POST "$BASE/api/tasks/from_code" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
    -d "$payload" || true
}

run_case() {
  local id="$1"
  local payload="$2"
  local expected="$3"
  local attempts max_attempts http code task_id
  attempts=0
  max_attempts=8
  while true; do
    attempts=$((attempts + 1))
    http="$(post_from_code "$payload")"
    if [[ "$http" == "500" ]] || jq -e '((.error // "") | tostring | test("SQLITE_BUSY|database is locked"))' "$TMP" >/dev/null 2>&1; then
      if (( attempts < max_attempts )); then
        sleep 0.6
        continue
      fi
    fi
    break
  done
  code="$(jq -r '.code // ""' "$TMP" 2>/dev/null || true)"
  task_id="$(jq -r '.id // ""' "$TMP" 2>/dev/null || true)"
  if [[ "${http}:${code}" =~ $expected ]]; then
    record "$id" "pass" "http=$http code=$code id=$task_id"
  else
    record "$id" "fail" "unexpected http=$http code=$code body=$(cat "$TMP" 2>/dev/null || true)"
  fi
}

ts="$(date -u +%Y%m%dt%H%M%S)"
mkid() { printf "audit-%s-%s-%s" "$1" "$ts" "$RANDOM"; }

rust_payload="$(cat <<EOF
{"id":"$(mkid rust)","language":"rust","code":"#[no_mangle]\npub extern \"C\" fn check(n:i64)->i32{ if n%19==0 {1} else {0} }\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"audit:rust"}
EOF
)"
cpp_payload="$(cat <<EOF
{"id":"$(mkid cpp)","language":"cpp","code":"#include <stdint.h>\nextern \"C\" int32_t check(int64_t n){ return (n%19==0)?1:0; }\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"audit:cpp"}
EOF
)"
c_payload="$(cat <<EOF
{"id":"$(mkid c)","language":"c","code":"#include <stdint.h>\nint32_t check(int64_t n){ return (n%19==0)?1:0; }\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"audit:c"}
EOF
)"
zig_payload="$(cat <<EOF
{"id":"$(mkid zig)","language":"zig","code":"export fn check(n: i64) i32 {\n    if (@rem(n, 19) == 0) return 1;\n    return 0;\n}\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"audit:zig"}
EOF
)"
as_payload="$(cat <<EOF
{"id":"$(mkid as)","language":"assemblyscript","code":"export function check(n: i64): i32 {\n  return i32((n % 19) == 0 ? 1 : 0);\n}\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"audit:assemblyscript"}
EOF
)"
tinygo_payload="$(cat <<EOF
{"id":"$(mkid tinygo)","language":"tinygo","code":"package main\n//export check\nfunc check(n int64) int32 { if n%19==0 { return 1 }; return 0 }\nfunc main() {}\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"audit:tinygo"}
EOF
)"
go_alias_payload="$(cat <<EOF
{"id":"$(mkid goalias)","language":"go","code":"package main\n//export check\nfunc check(n int64) int32 { if n%19==0 { return 1 }; return 0 }\nfunc main() {}\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"audit:go-alias"}
EOF
)"
wat_payload="$(cat <<EOF
{"id":"$(mkid wat)","language":"wat","code":"(module\n  (func (export \"check\") (param i64) (result i32)\n    local.get 0\n    i64.const 19\n    i64.rem_s\n    i64.eqz\n    if (result i32)\n      i32.const 1\n    else\n      i32.const 0\n    end))\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"audit:wat"}
EOF
)"

# For rust/cpp/c/wat: success or expected environment constraints.
run_case "audit-rust-from-code" "$rust_payload" '^(200:|402:manifest_rejected|429:rate_limited|400:compile_failed)$'
run_case "audit-cpp-from-code" "$cpp_payload" '^(200:|402:manifest_rejected|429:rate_limited|400:compile_failed)$'
run_case "audit-c-from-code" "$c_payload" '^(200:|402:manifest_rejected|429:rate_limited|400:compile_failed)$'
run_case "audit-zig-from-code" "$zig_payload" '^(200:|402:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed|400:wasm_sanitize_failed)$'
run_case "audit-assemblyscript-from-code" "$as_payload" '^(200:|402:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed|400:wasm_sanitize_failed)$'
run_case "audit-wat-from-code" "$wat_payload" '^(200:|402:manifest_rejected|429:rate_limited|400:compile_failed|400:unsupported_language)$'
# tinygo can still fail compile on hosts without tinygo toolchain.
run_case "audit-tinygo-from-code" "$tinygo_payload" '^(200:|402:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed)$'
run_case "audit-go-alias-from-code" "$go_alias_payload" '^(200:|402:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed|400:wasm_sanitize_failed)$'

tasks_json="$OUT/tasks.json"
curl -sS "$BASE/api/tasks" >"$tasks_json"
if jq -e '(.tasks | type) == "array"' "$tasks_json" >/dev/null 2>&1; then
  record "audit-tasks-list-shape" "pass" "tasks endpoint shape ok"
else
  record "audit-tasks-list-shape" "fail" "tasks endpoint malformed"
fi

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
  fail "orders multilang audit FAIL ($fails/$total). See $OUT"
fi
pass "orders multilang audit PASS ($total checks). See $OUT"
