#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

NODES="${NODES:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/rehearsal"
ensure_reports_dir "$OUT"

IFS=',' read -r -a nodes <<<"$NODES"
results="$OUT/results.jsonl"
: >"$results"

for node in "${nodes[@]}"; do
  node="$(echo "$node" | xargs)"
  [[ -z "$node" ]] && continue
  status="$(curl -sS "$node/api/status" || true)"
  metrics="$(curl -sS "$node/api/metrics" || true)"
  wallet="$(curl -sS "$node/api/wallet" || true)"
  ok="pass"
  tip="$(jq -r '.tip_height // -1' <<<"$status" 2>/dev/null || echo "-1")"
  mining="$(jq -r '.mining // false' <<<"$status" 2>/dev/null || echo "false")"
  addr="$(jq -r '.address // ""' <<<"$wallet" 2>/dev/null || true)"
  if [[ "$tip" == "-1" || -z "$addr" ]]; then
    ok="fail"
  fi
  jq -nc --arg node "$node" --arg verdict "$ok" --argjson tip_height "${tip:-0}" --arg mining "$mining" --arg address "$addr" \
    '{node:$node,verdict:$verdict,tip_height:$tip_height,mining:$mining,address:$address}' >>"$results"
  printf '%s\n' "$status" >"$OUT/$(echo "$node" | tr ':/' '_')_status.json"
  printf '%s\n' "$metrics" >"$OUT/$(echo "$node" | tr ':/' '_')_metrics.json"
done

fails="$(jq -r 'select(.verdict=="fail") | .node' "$results" | wc -l | tr -d ' ')"
total="$(wc -l <"$results" | tr -d ' ')"
jq -nc --arg run_id "$RID" --arg nodes "$NODES" --arg captured_at "$(ts_utc)" --argjson total "$total" --argjson fails "$fails" \
  '{run_id:$run_id,nodes:$nodes,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' >"$OUT/summary.json"

if [[ "$fails" != "0" ]]; then
  fail "rehearsal onboarding FAIL ($fails/$total). See $OUT"
fi
pass "rehearsal onboarding PASS ($total nodes). See $OUT"
