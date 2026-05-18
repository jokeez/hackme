#!/usr/bin/env bash
set -euo pipefail

# RC freeze gate:
# - single orchestration point before release-candidate lock
# - aggregates core readiness (internet + final preflight)
# - optionally includes fuzz gates
#
# Usage:
#   ADMIN_TOKEN=... BASE=http://127.0.0.1:8080 COORD=http://127.0.0.1:8081 \
#   VPS_BASE=http://132.243.112.100:18080 \
#   bash scripts/ops/rc_freeze_gate.sh
#
# Optional:
#   RUN_INTERNET_PREFLIGHT=1 RUN_FINAL_PREFLIGHT=1 RUN_FUZZ_RELEASE_GATE=0 RUN_FUZZ_SUPER_GATE=0

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

BASE="${BASE:-http://127.0.0.1:8080}"
VPS_BASE="${VPS_BASE:-http://132.243.112.100:18080}"
COORD="${COORD:-http://127.0.0.1:8081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/gates}"
RUN_ID="${RUN_ID:-rc_freeze_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="$OUT_DIR/$RUN_ID"
RESULTS="$OUT/results.jsonl"
mkdir -p "$OUT"
: >"$RESULTS"

RUN_INTERNET_PREFLIGHT="${RUN_INTERNET_PREFLIGHT:-1}"
RUN_FINAL_PREFLIGHT="${RUN_FINAL_PREFLIGHT:-1}"
RUN_FUZZ_RELEASE_GATE="${RUN_FUZZ_RELEASE_GATE:-0}"
RUN_FUZZ_SUPER_GATE="${RUN_FUZZ_SUPER_GATE:-0}"
INTERNET_REQUIRE_P2P="${INTERNET_REQUIRE_P2P:-1}"
INTERNET_MIN_HEALTHY_PEERS="${INTERNET_MIN_HEALTHY_PEERS:-1}"
INTERNET_MAX_SYNC_LAG_BLOCKS="${INTERNET_MAX_SYNC_LAG_BLOCKS:-3}"
INTERNET_REQUIRE_COORD_HEALTH="${INTERNET_REQUIRE_COORD_HEALTH:-1}"
INTERNET_RUN_PRIVATE_STAGE="${INTERNET_RUN_PRIVATE_STAGE:-1}"
INTERNET_RUN_DIFFICULTY_HEALTH="${INTERNET_RUN_DIFFICULTY_HEALTH:-1}"
SKIP_RELEASE_READINESS_GATE="${SKIP_RELEASE_READINESS_GATE:-0}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[rc-freeze] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd jq

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

if [[ "$RUN_INTERNET_PREFLIGHT" == "1" ]]; then
  if [[ -z "$ADMIN_TOKEN" ]]; then
    record "internet-preflight" "fail" "ADMIN_TOKEN/HACKME_ADMIN_TOKEN required for internet_preflight"
  else
    run_case "internet-preflight" "public/internet surface gate" \
      env BASE="$BASE" COORD="$COORD" ADMIN_TOKEN="$ADMIN_TOKEN" \
      REQUIRE_P2P="$INTERNET_REQUIRE_P2P" \
      MIN_HEALTHY_PEERS="$INTERNET_MIN_HEALTHY_PEERS" \
      MAX_SYNC_LAG_BLOCKS="$INTERNET_MAX_SYNC_LAG_BLOCKS" \
      REQUIRE_COORD_HEALTH="$INTERNET_REQUIRE_COORD_HEALTH" \
      RUN_PRIVATE_STAGE="$INTERNET_RUN_PRIVATE_STAGE" \
      RUN_DIFFICULTY_HEALTH="$INTERNET_RUN_DIFFICULTY_HEALTH" \
      RUN_ID="${RUN_ID}_internet" \
      bash scripts/ops/internet_preflight.sh
  fi
fi

if [[ "$RUN_FINAL_PREFLIGHT" == "1" ]]; then
  run_case "final-preflight" "core local+vps+coordinator readiness gate" \
    env BASE="$BASE" VPS_BASE="$VPS_BASE" COORD="$COORD" ADMIN_TOKEN="$ADMIN_TOKEN" \
    RUN_ID="${RUN_ID}_final" SKIP_RELEASE_READINESS_GATE="$SKIP_RELEASE_READINESS_GATE" \
    bash scripts/ops/final_preflight.sh
fi

if [[ "$RUN_FUZZ_RELEASE_GATE" == "1" ]]; then
  run_case "fuzz-release-gate" "fuzz campaign release gate" \
    env BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" RUN_ID="${RUN_ID}_fuzz_release" \
    bash scripts/ops/fuzz_release_gate.sh
fi

if [[ "$RUN_FUZZ_SUPER_GATE" == "1" ]]; then
  run_case "fuzz-super-gate" "extended fuzz integrity gate" \
    env BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" RUN_ID="${RUN_ID}_fuzz_super" \
    bash scripts/ops/fuzz_super_gate.sh
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"
total="$(wc -l <"$RESULTS" | tr -d ' ')"
summary="$OUT/summary.json"

jq -nc \
  --arg run_id "$RUN_ID" \
  --arg base "$BASE" \
  --arg vps_base "$VPS_BASE" \
  --arg coord "$COORD" \
  --arg captured_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson total "$total" \
  --argjson fails "$fails" \
  --argjson run_internet "$([[ "$RUN_INTERNET_PREFLIGHT" == "1" ]] && echo true || echo false)" \
  --argjson run_final "$([[ "$RUN_FINAL_PREFLIGHT" == "1" ]] && echo true || echo false)" \
  --argjson run_fuzz_release "$([[ "$RUN_FUZZ_RELEASE_GATE" == "1" ]] && echo true || echo false)" \
  --argjson run_fuzz_super "$([[ "$RUN_FUZZ_SUPER_GATE" == "1" ]] && echo true || echo false)" \
  '{
    gate: "rc_freeze_gate_v1",
    run_id:$run_id,
    captured_at:$captured_at,
    endpoints:{base:$base,vps_base:$vps_base,coord:$coord},
    checks:{
      internet_preflight:$run_internet,
      final_preflight:$run_final,
      fuzz_release_gate:$run_fuzz_release,
      fuzz_super_gate:$run_fuzz_super
    },
    total:$total,
    fails:$fails,
    status:(if $fails==0 then "PASS" else "FAIL" end)
  }' >"$summary"

echo "[rc-freeze] summary: $summary"
if [[ "$fails" != "0" ]]; then
  echo "[rc-freeze] FAIL ($fails/$total)"
  exit 1
fi
echo "[rc-freeze] PASS ($total checks)"
