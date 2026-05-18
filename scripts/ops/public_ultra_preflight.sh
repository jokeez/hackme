#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[public-ultra] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd curl
require_cmd jq

BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"
VPS_BASE="${VPS_BASE:-$BASE}"
P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
RUN_ID="${RUN_ID:-ultra_preflight_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests/$RUN_ID/public_ultra}"

mkdir -p "$OUT_DIR"
SUMMARY="$OUT_DIR/summary.json"
RESULTS="$OUT_DIR/results.jsonl"
: >"$RESULTS"

record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
}

run_case() {
  local id="$1"
  local detail="$2"
  shift 2
  local log_file="$OUT_DIR/${id}.log"
  if "$@" >"$log_file" 2>&1; then
    record "$id" "pass" "$detail"
  else
    record "$id" "fail" "$detail (see $log_file)"
  fi
}

run_case "health-status" "status endpoint responds" curl -fsS "$BASE/api/status"
run_case "health-global" "global metrics responds" curl -fsS "$BASE/api/global/metrics"
run_case "health-work" "coordinator work stats responds" curl -fsS "$COORD/api/work/stats"

run_case "consensus-policy-visible" "status exposes simultaneous block rule" bash -lc \
  "curl -fsS '$BASE/api/status' | jq -e '.consensus_policy.simultaneous_block_rule == \"first_valid_block_on_canonical_node_wins\"' >/dev/null"

if [[ -n "$P2P_TOKEN" ]]; then
  run_case "p2p-peer-snapshot" "p2p peers endpoint responds with token" bash -lc \
    "curl -fsS -H 'X-Hackme-P2P-Token: $P2P_TOKEN' '$BASE/api/p2p/peers' >/dev/null"
else
  record "p2p-peer-snapshot" "pass" "skipped: P2P_TOKEN/HACKME_P2P_TOKEN not set"
fi

if [[ -x "$ROOT_DIR/scripts/ops/internet_preflight.sh" ]]; then
  run_case "internet-preflight" "network/security preflight" env \
    ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$BASE" COORD="$COORD" \
    "$ROOT_DIR/scripts/ops/internet_preflight.sh"
fi

if [[ -x "$ROOT_DIR/scripts/tests/public_release_mega_pack.sh" ]]; then
  run_case "public-release-mega-pack" "mega validation bundle" env \
    RUN_ID="${RUN_ID}_mega" BASE="$BASE" COORD="$COORD" P2P_TOKEN="$P2P_TOKEN" \
    ULTIMATE_MEGA_DURATION="${ULTIMATE_MEGA_DURATION:-120}" \
    ULTIMATE_PRE_DURATION="${ULTIMATE_PRE_DURATION:-180}" \
    ULTIMATE_PRE_INTERVAL="${ULTIMATE_PRE_INTERVAL:-30}" \
    "$ROOT_DIR/scripts/tests/public_release_mega_pack.sh"
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"
total="$(wc -l <"$RESULTS" | tr -d ' ')"
jq -nc --arg run_id "$RUN_ID" --arg base "$BASE" --arg coord "$COORD" \
  --arg captured_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson total "$total" --argjson fails "$fails" \
  '{run_id:$run_id,base:$base,coord:$coord,captured_at:$captured_at,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' \
  >"$SUMMARY"

echo "[public-ultra] summary: $SUMMARY"
if [[ "$fails" != "0" ]]; then
  echo "[public-ultra] FAIL ($fails/$total)"
  exit 1
fi
echo "[public-ultra] PASS ($total checks)"

