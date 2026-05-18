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
OUT="$OUT_DIR/$RID/language_from_code"
ensure_reports_dir "$OUT"
RESULTS="$OUT/results.jsonl"
: >"$RESULTS"

if [[ -z "$ADMIN_TOKEN" ]]; then
  fail "language matrix: ADMIN_TOKEN/HACKME_ADMIN_TOKEN is required"
fi

tmp="$OUT/tmp.json"

record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
}

post_from_code() {
  local payload="$1"
  curl -sS -o "$tmp" -w '%{http_code}' -X POST "$BASE/api/tasks/from_code" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
    -d "$payload" || true
}

run_and_check() {
  local id="$1"
  local payload="$2"
  local expected_regex="$3"
  local note="$4"
  local http code attempts max_attempts
  attempts=0
  max_attempts=8
  while true; do
    attempts=$((attempts + 1))
    http="$(post_from_code "$payload")"
    # Handle transient SQLite contention from concurrent tests/gates.
    if [[ "$http" == "500" ]] || jq -e '((.error // "") | tostring | test("SQLITE_BUSY|database is locked"))' "$tmp" >/dev/null 2>&1; then
      if (( attempts < max_attempts )); then
        sleep 0.6
        continue
      fi
    fi
    break
  done
  code="$(jq -r '.code // ""' "$tmp" 2>/dev/null || true)"
  if [[ "${http}:${code}" =~ $expected_regex ]]; then
    record "$id" "pass" "$note (http=$http code=$code)"
  else
    record "$id" "fail" "$note (unexpected http=$http code=$code body=$(cat "$tmp" 2>/dev/null || true))"
  fi
}

ts="$(date -u +%Y%m%dt%H%M%S)"

rust_payload="$(cat <<EOF
{"id":"lang-rust-${ts}","language":"rust","code":"#[no_mangle]\npub extern \"C\" fn check(n: i64) -> i32 { if n % 17 == 0 { 1 } else { 0 } }\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:rust"}
EOF
)"

cpp_payload="$(cat <<EOF
{"id":"lang-cpp-${ts}","language":"cpp","code":"#include <stdint.h>\nextern \"C\" int32_t check(int64_t n){ return (n%17==0)?1:0; }\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:cpp"}
EOF
)"

c_payload="$(cat <<EOF
{"id":"lang-c-${ts}","language":"c","code":"#include <stdint.h>\nint32_t check(int64_t n){ return (n%17==0)?1:0; }\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:c"}
EOF
)"

gcc_alias_payload="$(cat <<EOF
{"id":"lang-gcc-alias-${ts}","language":"gcc","code":"#include <stdint.h>\nint32_t check(int64_t n){ return (n%17==0)?1:0; }\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:gcc-alias"}
EOF
)"

zig_payload="$(cat <<EOF
{"id":"lang-zig-${ts}","language":"zig","code":"export fn check(n: i64) i32 {\n    if (@rem(n, 17) == 0) return 1;\n    return 0;\n}\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:zig"}
EOF
)"

as_payload="$(cat <<EOF
{"id":"lang-as-${ts}","language":"assemblyscript","code":"export function check(n: i64): i32 {\n  return i32((n % 17) == 0 ? 1 : 0);\n}\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:assemblyscript"}
EOF
)"

as_alias_payload="$(cat <<EOF
{"id":"lang-as-alias-${ts}","language":"as","code":"export function check(n: i64): i32 {\n  return i32((n % 17) == 0 ? 1 : 0);\n}\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:as-alias"}
EOF
)"

tinygo_payload="$(cat <<EOF
{"id":"lang-tinygo-${ts}","language":"tinygo","code":"package main\n//export check\nfunc check(n int64) int32 { if n%17==0 { return 1 }; return 0 }\nfunc main() {}\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:tinygo"}
EOF
)"

go_alias_payload="$(cat <<EOF
{"id":"lang-go-alias-${ts}","language":"go","code":"package main\n//export check\nfunc check(n int64) int32 { if n%17==0 { return 1 }; return 0 }\nfunc main() {}\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:go-alias"}
EOF
)"

cpp_alias_payload="$(cat <<EOF
{"id":"lang-cxx-alias-${ts}","language":"c++","code":"#include <stdint.h>\nextern \"C\" int32_t check(int64_t n){ return (n%17==0)?1:0; }\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:cxx-alias"}
EOF
)"

wat_payload="$(cat <<EOF
{"id":"lang-wat-${ts}","language":"wat","code":"(module\n  (func (export \"check\") (param i64) (result i32)\n    local.get 0\n    i64.const 17\n    i64.rem_s\n    i64.eqz\n    if (result i32)\n      i32.const 1\n    else\n      i32.const 0\n    end))\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:wat"}
EOF
)"

badlang_payload='{"id":"lang-badlang","language":"python","code":"print(1)","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:bad"}'
badrust_payload='{"id":"lang-rust-bad","language":"rust","code":"#[no_mangle] pub extern \"C\" fn check( -> i32 {","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"lang:rust-bad"}'

# rust/cpp can legitimately end with created order (200), insufficient balance (402), rate-limit (429),
# or compile/toolchain issues on nodes without compiler (400 compile_failed).
run_and_check "from-code-rust" "$rust_payload" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed)$' \
  "rust from_code contract"

run_and_check "from-code-cpp" "$cpp_payload" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed)$' \
  "cpp from_code contract"

run_and_check "from-code-c" "$c_payload" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed)$' \
  "c from_code contract"

run_and_check "from-code-gcc-alias" "$gcc_alias_payload" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed)$' \
  "gcc alias -> c contract"

run_and_check "from-code-zig" "$zig_payload" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed|400:wasm_sanitize_failed)$' \
  "zig from_code contract (sanitize + strict ABI)"

run_and_check "from-code-assemblyscript" "$as_payload" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed|400:wasm_sanitize_failed)$' \
  "assemblyscript from_code contract (sanitize + strict ABI)"

run_and_check "from-code-as-alias" "$as_alias_payload" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed|400:wasm_sanitize_failed)$' \
  "as alias -> assemblyscript contract"

# tinygo should pass strict ABI after sanitize; compile/toolchain may still fail on hosts without tinygo.
run_and_check "from-code-tinygo" "$tinygo_payload" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed)$' \
  "tinygo from_code strict contract"

run_and_check "from-code-go-alias" "$go_alias_payload" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed|400:wasm_sanitize_failed)$' \
  "go alias -> tinygo contract"

run_and_check "from-code-cpp-alias" "$cpp_alias_payload" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed)$' \
  "c++ alias -> cpp contract"

run_and_check "from-code-wat" "$wat_payload" '^(200:|402:manifest_rejected|400:manifest_rejected|429:rate_limited|400:compile_failed|400:wasm_validation_failed|400:unsupported_language)$' \
  "wat/assembly from_code contract (or pre-deploy unsupported)"

run_and_check "from-code-unsupported-language" "$badlang_payload" '^(400:unsupported_language|429:rate_limited)$' \
  "unsupported language rejection"

run_and_check "from-code-rust-compile-fail" "$badrust_payload" '^(400:compile_failed|429:rate_limited)$' \
  "rust invalid source compile failure"

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
  fail "language from_code matrix FAIL ($fails/$total). See $OUT"
fi
pass "language from_code matrix PASS ($total checks). See $OUT"
