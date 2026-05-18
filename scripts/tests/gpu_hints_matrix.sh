#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd go
require_cmd jq

OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/gpu_hints_matrix"
ensure_reports_dir "$OUT"

json_log="$OUT/go_test.jsonl"
go test -json ./internal/gputune | tee "$json_log" >/dev/null

total_cases="$(jq -r 'select(.Action=="pass" and (.Test != null) and (.Test|startswith("TestForGPUName_"))) | .Test' "$json_log" | wc -l | tr -d ' ')"
failed_cases="$(jq -r 'select(.Action=="fail" and (.Test != null) and (.Test|startswith("TestForGPUName_"))) | .Test' "$json_log" | wc -l | tr -d ' ')"
status="PASS"
if [[ "$failed_cases" != "0" ]]; then
  status="FAIL"
fi

jq -nc \
  --arg run_id "$RID" \
  --arg captured_at "$(ts_utc)" \
  --arg status "$status" \
  --argjson total_cases "${total_cases:-0}" \
  --argjson failed_cases "${failed_cases:-0}" \
  '{
    run_id:$run_id,
    captured_at:$captured_at,
    suite:"gpu_hints_matrix",
    total:$total_cases,
    fails:$failed_cases,
    total_cases:$total_cases,
    failed_cases:$failed_cases,
    status:$status
  }' >"$OUT/summary.json"

if [[ "$status" != "PASS" ]]; then
  fail "gpu hints matrix FAIL ($failed_cases/$total_cases). See $OUT"
fi
pass "gpu hints matrix PASS ($total_cases cases). See $OUT"
