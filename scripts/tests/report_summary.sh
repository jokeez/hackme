#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "Missing command: $1" >&2; exit 1; }; }
require_cmd jq

OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-}"

if [[ -z "$RID" ]]; then
  RID="$(ls -1 "$OUT_DIR" 2>/dev/null | sort | tail -n 1 || true)"
fi
[[ -n "$RID" ]] || { echo "No run id found in $OUT_DIR" >&2; exit 1; }

RUN_DIR="$OUT_DIR/$RID"
[[ -d "$RUN_DIR" ]] || { echo "Run dir not found: $RUN_DIR" >&2; exit 1; }

summaries=()
while IFS= read -r f; do
  summaries+=("$f")
done < <(ls -1 "$RUN_DIR"/*/summary.json 2>/dev/null || true)

[[ "${#summaries[@]}" -gt 0 ]] || { echo "No summary.json files under $RUN_DIR" >&2; exit 1; }

jq -s \
  --arg run_id "$RID" \
  --argjson paths "$(printf '%s\n' "${summaries[@]}" | jq -R . | jq -s .)" \
  '{
    run_id: $run_id,
    generated_at: (now | todateiso8601),
    suites: [range(0; length) as $i | {
      path: $paths[$i],
      status: (.[ $i ].status // "UNKNOWN"),
      total: (.[ $i ].total // .[ $i ].total_cases // 0),
      fails: (.[ $i ].fails // .[ $i ].failed_cases // 0)
    }],
    total_cases: (map((.total // .total_cases // 0)) | add),
    total_fails: (map((.fails // .failed_cases // 0)) | add),
    status: (if (map((.fails // .failed_cases // 0)) | add) == 0 then "PASS" else "FAIL" end)
  }' "${summaries[@]}" >"$RUN_DIR/summary_all.json"

echo "Wrote: $RUN_DIR/summary_all.json"
jq '.' "$RUN_DIR/summary_all.json"
