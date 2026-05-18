#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

BASE="${BASE:-http://127.0.0.1:8080}"
VPS_BASE="${VPS_BASE:-http://132.243.112.100:18080}"
COORD="${COORD:-http://132.243.112.100:18081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RUN_ID="${RUN_ID:-final_preflight_$(date -u +%Y%m%dT%H%M%SZ)}"
SKIP_RELEASE_READINESS_GATE="${SKIP_RELEASE_READINESS_GATE:-0}"
FULL_RUN_ID="${FULL_RUN_ID:-}"
ADV_RUN_ID="${ADV_RUN_ID:-}"
PRE_RUN_ID="${PRE_RUN_ID:-}"
MEGA_RUN_ID="${MEGA_RUN_ID:-}"
OUT="$OUT_DIR/$RUN_ID/final_preflight"
mkdir -p "$OUT"
RESULTS="$OUT/results.jsonl"
: >"$RESULTS"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[final-preflight] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq
require_cmd bash

record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
}

run_case() {
  local id="$1"
  local detail="$2"
  shift 2
  local log_file="$OUT/${id}.log"
  if "$@" >"$log_file" 2>&1; then
    record "$id" "pass" "$detail"
  else
    record "$id" "fail" "$detail (see $log_file)"
  fi
}

run_case "local-status" "local status endpoint reachable" \
  curl_retry_fsS -fsS "$BASE/api/status"
run_case "vps-status" "vps status endpoint reachable" \
  curl_retry_fsS -fsS "$VPS_BASE/api/status"
run_case "coord-work-stats" "coordinator work stats endpoint reachable" \
  curl_retry_fsS -fsS "$COORD/api/work/stats"

run_case "strict-network-preflight" "strict p2p/coordinator gate" \
  env BASE="$BASE" COORD="$COORD" RUN_ID="${RUN_ID}_strict" \
  bash scripts/ops/strict_network_preflight.sh

if [[ -n "$ADMIN_TOKEN" ]]; then
  run_case "private-stage-gate" "private stage gate with coordinator health required" \
    env BASE="$BASE" COORD="$COORD" ADMIN_TOKEN="$ADMIN_TOKEN" \
    REQUIRE_COORD_HEALTH=1 DO_FREEZE=0 DO_BACKUP=0 RUN_ID="${RUN_ID}_private" \
    bash scripts/ops/private_stage_gate.sh
else
  record "private-stage-gate" "fail" "ADMIN_TOKEN/HACKME_ADMIN_TOKEN is required"
fi

run_case "difficulty-health" "difficulty retarget and target bounds checks" \
  env BASE="$BASE" RUN_ID="${RUN_ID}_difficulty" \
  bash scripts/tests/difficulty_health.sh

run_case "security-assertions" "economics invariants, transfer validation, RL smoke" \
  env BASE="$BASE" RUN_ID="${RUN_ID}_security" \
  bash scripts/tests/security_assertions.sh

if [[ "$SKIP_RELEASE_READINESS_GATE" == "1" ]]; then
  record "release-readiness-gate" "pass" "skipped (SKIP_RELEASE_READINESS_GATE=1 — no prior full/adv/pre/mega artifacts)"
else
  run_case "release-readiness-gate" "aggregated suites readiness check" \
    env OUT_DIR="$OUT_DIR" RUN_ID="${RUN_ID}_readiness" \
      FULL_RUN_ID="$FULL_RUN_ID" ADV_RUN_ID="$ADV_RUN_ID" PRE_RUN_ID="$PRE_RUN_ID" MEGA_RUN_ID="$MEGA_RUN_ID" \
    bash scripts/tests/release_readiness_gate.sh
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"
total="$(wc -l <"$RESULTS" | tr -d ' ')"
jq -nc \
  --arg run_id "$RUN_ID" \
  --arg base "$BASE" \
  --arg vps_base "$VPS_BASE" \
  --arg coord "$COORD" \
  --arg captured_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson total "$total" \
  --argjson fails "$fails" \
  '{run_id:$run_id,base:$base,vps_base:$vps_base,coord:$coord,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' \
  >"$OUT/summary.json"

echo "[final-preflight] summary: $OUT/summary.json"
if [[ "$fails" != "0" ]]; then
  echo "[final-preflight] FAIL ($fails/$total)"
  exit 1
fi
echo "[final-preflight] PASS ($total checks)"
