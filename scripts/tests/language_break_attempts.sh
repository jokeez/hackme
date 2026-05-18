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
OUT="$OUT_DIR/$RID/language_break_attempts"
ensure_reports_dir "$OUT"
RESULTS="$OUT/results.jsonl"
: >"$RESULTS"
TMP="$OUT/tmp.json"

if [[ -z "$ADMIN_TOKEN" ]]; then
  fail "language_break_attempts: ADMIN_TOKEN/HACKME_ADMIN_TOKEN is required"
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

post_from_code_file() {
  local payload_file="$1"
  curl -sS -o "$TMP" -w '%{http_code}' -X POST "$BASE/api/tasks/from_code" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
    --data-binary "@${payload_file}" || true
}

run_case() {
  local id="$1"
  local payload="$2"
  local expected="$3"
  local attempts max_attempts http code
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
  if [[ "${http}:${code}" =~ $expected ]]; then
    record "$id" "pass" "http=$http code=$code"
  else
    record "$id" "fail" "unexpected http=$http code=$code body=$(cat "$TMP" 2>/dev/null || true)"
  fi
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
    http="$(post_from_code_file "$payload_file")"
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

ts="$(date -u +%Y%m%dt%H%M%S)"
mkid() { printf "break-%s-%s-%s" "$1" "$ts" "$RANDOM"; }

# 1) WAT tries to export additional memory symbol -> must fail strict export gate.
wat_extra_export="$(cat <<EOF
{"id":"$(mkid wat-extra-export)","language":"wat","code":"(module\n (memory 1)\n (func (export \"check\") (param i64) (result i32)\n  i32.const 1)\n (export \"mem\" (memory 0))\n)\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"break:wat-extra-export"}
EOF
)"
run_case "break-wat-extra-export" "$wat_extra_export" '^(400:wasm_validation_failed|400:compile_failed|429:rate_limited)$'

# 2) WAT with start section attempting heavy loop should fail validation/instantiate timeout.
wat_start_loop="$(cat <<EOF
{"id":"$(mkid wat-start-loop)","language":"wat","code":"(module\n  (func \$spin (loop br 0))\n  (start \$spin)\n  (func (export \"check\") (param i64) (result i32)\n    i32.const 1)\n)\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"break:wat-start-loop"}
EOF
)"
run_case "break-wat-start-loop" "$wat_start_loop" '^(400:wasm_validation_failed|400:compile_failed|429:rate_limited)$'

# 3) TinyGo code without check export should fail sanitize/validation.
tinygo_no_check="$(cat <<EOF
{"id":"$(mkid tinygo-no-check)","language":"tinygo","code":"package main\nfunc nope(n int64) int32 { return 1 }\nfunc main() {}\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"break:tinygo-no-check"}
EOF
)"
run_case "break-tinygo-no-check" "$tinygo_no_check" '^(400:wasm_sanitize_failed|400:compile_failed|400:wasm_validation_failed|429:rate_limited)$'

# 4) Oversized source should be rejected before compile.
big_payload_file="$OUT/big_payload.json"
python3 - <<'PY' > "$big_payload_file"
import json, random, time
payload = {
    "id": f"break-code-too-large-{time.strftime('%Y%m%dt%H%M%S')}-{random.randint(1000,9999)}",
    "language": "wat",
    "code": "A" * 210000,
    "reward_hmc": 0.01,
    "difficulty_score": 1,
    "target_solves": 1,
    "payer_ref": "break:too-large",
}
print(json.dumps(payload))
PY
run_case_file "break-code-too-large" "$big_payload_file" '^(400:code_too_large|429:rate_limited)$'

# 5) Rust source with suspicious shell-like payload must not execute anything; compile should fail.
rust_injection="$(cat <<EOF
{"id":"$(mkid rust-injection)","language":"rust","code":"#[no_mangle]\npub extern \"C\" fn check(n:i64)->i32 { let _x = \"\\$(rm -rf /tmp/never-run)\"; if n>0 {1}else{0}}\n","reward_hmc":0.01,"difficulty_score":1,"target_solves":1,"payer_ref":"break:rust-injection"}
EOF
)"
run_case "break-rust-source-injection" "$rust_injection" '^(200:|400:compile_failed|400:invalid_json|402:manifest_rejected|429:rate_limited)$'

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
  fail "language break attempts FAIL ($fails/$total). See $OUT"
fi
pass "language break attempts PASS ($total checks). See $OUT"
