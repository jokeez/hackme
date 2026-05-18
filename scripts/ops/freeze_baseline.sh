#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_BASE="${OUT_BASE:-$ROOT_DIR/reports/baseline-freeze}"
RID="${RUN_ID:-freeze_$(date -u +%Y%m%dT%H%M%SZ)}"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[freeze] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq

OUT="$OUT_BASE/$RID"
mkdir -p "$OUT"

echo "[freeze] writing baseline snapshot to $OUT"

curl -fsS "$BASE/api/status" | jq '.' >"$OUT/status.json"
curl -fsS "$BASE/api/metrics" | jq '.' >"$OUT/metrics.json"
curl -fsS "$BASE/api/network/stats" | jq '.' >"$OUT/network_stats.json"
curl -fsS "$BASE/api/work/stats" | jq '.' >"$OUT/work_stats.json"
curl -fsS "$BASE/api/p2p/peers" | jq '.' >"$OUT/p2p_peers.json"
coord_tmp="$OUT/coordinator_work_stats.raw.json"
coord_http="$(curl -sS -o "$coord_tmp" -w '%{http_code}' "$COORD/api/work/stats" || true)"
if [[ "$coord_http" == "405" ]]; then
  coord_http="$(curl -sS -o "$coord_tmp" -w '%{http_code}' -X POST "$COORD/api/work/stats" || true)"
fi
if [[ "$coord_http" == "200" ]]; then
  jq '.' "$coord_tmp" >"$OUT/coordinator_work_stats.json"
else
  jq -nc --arg http "$coord_http" --arg response "$(cat "$coord_tmp" 2>/dev/null || true)" \
    '{ok:false, http:$http, error:"coordinator_work_stats_unavailable", response:$response}' >"$OUT/coordinator_work_stats.json"
fi
rm -f "$coord_tmp"

jq -nc \
  --arg run_id "$RID" \
  --arg captured_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg base "$BASE" \
  --arg coord "$COORD" \
  '{run_id:$run_id,captured_at:$captured_at,base:$base,coord:$coord,status:"FROZEN"}' >"$OUT/manifest.json"

echo "[freeze] done: $OUT"

