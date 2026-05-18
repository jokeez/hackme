#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd jq

OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/p2p_quality_matrix"
ensure_reports_dir "$OUT"
results="$OUT/results.jsonl"
: >"$results"

record_case() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$results"
}

evaluate_gate() {
  local peers_file="$1" max_unstable="$2" max_bad="$3" out_file="$4"
  jq -c --argjson max_unstable "$max_unstable" --argjson max_bad "$max_bad" '
    . as $root
    | ($root.peers // []) as $peers
    | {
        total: ($peers | length),
        healthy: ($peers | map(select(.healthy == true)) | length),
        unstable: ($peers | map(select(.unstable == true)) | length),
        bad: ($peers | map(select((.quality // "unknown") == "bad")) | length)
      } as $c
    | $c + {
        pass_all_down: (if $c.total == 0 then true else ($c.healthy > 0) end),
        pass_unstable_budget: ($c.unstable <= $max_unstable),
        pass_bad_budget: ($c.bad <= $max_bad)
      }
  ' "$peers_file" >"$out_file"
}

run_case() {
  local id="$1" peers_json="$2" max_unstable="$3" max_bad="$4"
  local expect_all_down="$5" expect_unstable="$6" expect_bad="$7"
  local peers_file="$OUT/${id}_peers.json"
  local gate_file="$OUT/${id}_gate.json"
  printf '%s\n' "$peers_json" >"$peers_file"
  evaluate_gate "$peers_file" "$max_unstable" "$max_bad" "$gate_file"

  local got_all_down got_unstable got_bad verdict
  got_all_down="$(jq -r '.pass_all_down' "$gate_file")"
  got_unstable="$(jq -r '.pass_unstable_budget' "$gate_file")"
  got_bad="$(jq -r '.pass_bad_budget' "$gate_file")"
  verdict="pass"
  if [[ "$got_all_down" != "$expect_all_down" || "$got_unstable" != "$expect_unstable" || "$got_bad" != "$expect_bad" ]]; then
    verdict="fail"
  fi
  record_case "$id" "$verdict" "$(jq -c --arg expect_all_down "$expect_all_down" --arg expect_unstable "$expect_unstable" --arg expect_bad "$expect_bad" \
    '. + {expect_all_down:$expect_all_down,expect_unstable:$expect_unstable,expect_bad:$expect_bad}' "$gate_file")"
}

run_case "case-no-peers" '{"enabled":true,"peers":[]}' 0 0 "true" "true" "true"
run_case "case-healthy-good" '{"enabled":true,"peers":[{"healthy":true,"unstable":false,"quality":"good"}]}' 0 0 "true" "true" "true"
run_case "case-all-down-detected" '{"enabled":true,"peers":[{"healthy":false,"unstable":true,"quality":"bad"},{"healthy":false,"unstable":false,"quality":"warning"}]}' 1 1 "false" "true" "true"
run_case "case-budget-overflow-detected" '{"enabled":true,"peers":[{"healthy":true,"unstable":true,"quality":"warning"},{"healthy":true,"unstable":true,"quality":"bad"}]}' 0 0 "true" "false" "false"

fails="$(jq -r 'select(.verdict=="fail") | .id' "$results" | wc -l | tr -d ' ')"
total="$(wc -l <"$results" | tr -d ' ')"
jq -nc --arg run_id "$RID" --arg captured_at "$(ts_utc)" --argjson total "$total" --argjson fails "$fails" \
  '{run_id:$run_id,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' >"$OUT/summary.json"

if [[ "$fails" != "0" ]]; then
  fail "p2p quality matrix FAIL ($fails/$total). See $OUT"
fi
pass "p2p quality matrix PASS ($total checks). See $OUT"
