#!/usr/bin/env bash
set -euo pipefail

# Nightly RC freeze runner:
# - executes rc_freeze_gate
# - writes a compact markdown report for humans
# - keeps machine-readable artifacts for automation
#
# Usage:
#   ADMIN_TOKEN=... BASE=http://127.0.0.1:8080 COORD=http://127.0.0.1:8081 \
#   VPS_BASE=http://132.243.112.100:18080 \
#   bash scripts/ops/rc_freeze_nightly.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"
VPS_BASE="${VPS_BASE:-http://132.243.112.100:18080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"

OUT_BASE="${OUT_BASE:-$ROOT_DIR/reports/gates}"
RUN_ID="${RUN_ID:-rc_nightly_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="$OUT_BASE/$RUN_ID"
mkdir -p "$OUT"

RUN_INTERNET_PREFLIGHT="${RUN_INTERNET_PREFLIGHT:-1}"
RUN_FINAL_PREFLIGHT="${RUN_FINAL_PREFLIGHT:-1}"
RUN_FUZZ_RELEASE_GATE="${RUN_FUZZ_RELEASE_GATE:-0}"
RUN_FUZZ_SUPER_GATE="${RUN_FUZZ_SUPER_GATE:-0}"
PROFILE="${PROFILE:-practical}"

# practical (default): stable nightly signal on single-VPS topology
# strict: require strict peer/readiness behavior
if [[ "$PROFILE" == "practical" ]]; then
  RUN_INTERNET_PREFLIGHT=1
  RUN_FINAL_PREFLIGHT=0
  INTERNET_REQUIRE_P2P="${INTERNET_REQUIRE_P2P:-0}"
  INTERNET_MIN_HEALTHY_PEERS="${INTERNET_MIN_HEALTHY_PEERS:-0}"
  INTERNET_REQUIRE_COORD_HEALTH="${INTERNET_REQUIRE_COORD_HEALTH:-1}"
  INTERNET_RUN_PRIVATE_STAGE="${INTERNET_RUN_PRIVATE_STAGE:-0}"
  INTERNET_RUN_DIFFICULTY_HEALTH="${INTERNET_RUN_DIFFICULTY_HEALTH:-1}"
else
  INTERNET_REQUIRE_P2P="${INTERNET_REQUIRE_P2P:-1}"
  INTERNET_MIN_HEALTHY_PEERS="${INTERNET_MIN_HEALTHY_PEERS:-1}"
  INTERNET_REQUIRE_COORD_HEALTH="${INTERNET_REQUIRE_COORD_HEALTH:-1}"
  INTERNET_RUN_PRIVATE_STAGE="${INTERNET_RUN_PRIVATE_STAGE:-1}"
  INTERNET_RUN_DIFFICULTY_HEALTH="${INTERNET_RUN_DIFFICULTY_HEALTH:-1}"
fi

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[rc-nightly] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd jq

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[rc-nightly] ADMIN_TOKEN/HACKME_ADMIN_TOKEN is required" >&2
  exit 2
fi

gate_log="$OUT/rc_freeze_gate.log"
gate_run_id="${RUN_ID}_gate"

if env \
  BASE="$BASE" COORD="$COORD" VPS_BASE="$VPS_BASE" ADMIN_TOKEN="$ADMIN_TOKEN" \
  RUN_ID="$gate_run_id" OUT_DIR="$OUT_BASE" \
  RUN_INTERNET_PREFLIGHT="$RUN_INTERNET_PREFLIGHT" \
  RUN_FINAL_PREFLIGHT="$RUN_FINAL_PREFLIGHT" \
  RUN_FUZZ_RELEASE_GATE="$RUN_FUZZ_RELEASE_GATE" \
  RUN_FUZZ_SUPER_GATE="$RUN_FUZZ_SUPER_GATE" \
  INTERNET_REQUIRE_P2P="$INTERNET_REQUIRE_P2P" \
  INTERNET_MIN_HEALTHY_PEERS="$INTERNET_MIN_HEALTHY_PEERS" \
  INTERNET_REQUIRE_COORD_HEALTH="$INTERNET_REQUIRE_COORD_HEALTH" \
  INTERNET_RUN_PRIVATE_STAGE="$INTERNET_RUN_PRIVATE_STAGE" \
  INTERNET_RUN_DIFFICULTY_HEALTH="$INTERNET_RUN_DIFFICULTY_HEALTH" \
  bash "$ROOT_DIR/scripts/ops/rc_freeze_gate.sh" >"$gate_log" 2>&1; then
  gate_exit=0
else
  gate_exit=$?
fi

gate_out="$OUT_BASE/$gate_run_id"
summary_json="$gate_out/summary.json"
results_jsonl="$gate_out/results.jsonl"
nightly_summary="$OUT/nightly_summary.json"
nightly_md="$OUT/nightly_report.md"

if [[ ! -f "$summary_json" || ! -f "$results_jsonl" ]]; then
  echo "[rc-nightly] missing rc_freeze artifacts under $gate_out" >&2
  exit 1
fi

status="$(jq -r '.status // "UNKNOWN"' "$summary_json")"
total="$(jq -r '.total // 0' "$summary_json")"
fails="$(jq -r '.fails // 0' "$summary_json")"
captured_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

jq -nc \
  --arg run_id "$RUN_ID" \
  --arg gate_run_id "$gate_run_id" \
  --arg captured_at "$captured_at" \
  --arg base "$BASE" \
  --arg coord "$COORD" \
  --arg vps_base "$VPS_BASE" \
  --arg status "$status" \
  --argjson total "$total" \
  --argjson fails "$fails" \
  --argjson gate_exit "$gate_exit" \
  '{
    gate:"rc_freeze_nightly_v1",
    run_id:$run_id,
    gate_run_id:$gate_run_id,
    captured_at:$captured_at,
    endpoints:{base:$base,coord:$coord,vps_base:$vps_base},
    status:$status,
    total:$total,
    fails:$fails,
    gate_exit:$gate_exit
  }' >"$nightly_summary"

{
  echo "# RC Nightly Report"
  echo
  echo "- run_id: \`$RUN_ID\`"
  echo "- gate_run_id: \`$gate_run_id\`"
  echo "- captured_at: \`$captured_at\`"
  echo "- status: **$status**"
  echo "- checks: total=$total, fails=$fails"
  echo "- profile: \`$PROFILE\`"
  echo
  echo "## Endpoints"
  echo
  echo "- base: \`$BASE\`"
  echo "- coord: \`$COORD\`"
  echo "- vps_base: \`$VPS_BASE\`"
  echo
  echo "## Check Results"
  echo
  jq -rs '(["id","verdict","detail"] | @tsv), (.[] | [ .id, .verdict, .detail ] | @tsv)' "$results_jsonl" \
    | awk 'BEGIN{FS="\t"} NR==1{print "| Check | Verdict | Detail |"; print "|---|---|---|"; next} {gsub(/\|/, "\\|", $3); printf("| `%s` | **%s** | %s |\n", $1, toupper($2), $3)}'
  echo
  echo "## Artifacts"
  echo
  echo "- rc_freeze_summary: \`$summary_json\`"
  echo "- rc_freeze_results: \`$results_jsonl\`"
  echo "- gate_log: \`$gate_log\`"
  echo "- nightly_summary: \`$nightly_summary\`"
} >"$nightly_md"

echo "[rc-nightly] report: $nightly_md"
echo "[rc-nightly] summary: $nightly_summary"
if [[ "$status" != "PASS" ]]; then
  echo "[rc-nightly] FAIL (see artifacts under $OUT)"
  exit 1
fi
echo "[rc-nightly] PASS"
