#!/usr/bin/env bash
# Long-run HTTP probe: /api/status + /api/global/metrics (+ optional coordinator work/stats).
# Builds a timeline to judge how stable the public stack is (errors, latency, tip monotonicity).
#
# Usage (repo root):
#   BASE=https://hackme.tech DURATION_SEC=3600 INTERVAL_SEC=30 bash scripts/ops/network_stability_soak.sh
#
# Optional:
#   COORD_URL=https://hackme.tech/pool/coordinator   — light GET work/stats?details=0 (often 200 without token)
#   COORD_ADMIN_TOKEN=...  — if your coordinator requires admin for stats
#
# Reports: reports/soak-<RUN_ID>/events.jsonl + summary.txt

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "[soak] missing: $1" >&2; exit 1; }; }
require_cmd curl
require_cmd jq
require_cmd awk

BASE="${BASE:-https://hackme.tech}"
COORD_URL="${COORD_URL:-}"
COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-${ADMIN_TOKEN:-}}"
DURATION_SEC="${DURATION_SEC:-1800}"
INTERVAL_SEC="${INTERVAL_SEC:-30}"
RUN_ID="${RUN_ID:-soak_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/soak-$RUN_ID}"
mkdir -p "$OUT_DIR"

EVENTS="$OUT_DIR/events.jsonl"
SUMMARY="$OUT_DIR/summary.txt"
: >"$EVENTS"

base_trim="${BASE%/}"
deadline=$(( $(date +%s) + DURATION_SEC ))
iter=0
fail_http=0
ok_http=0
max_lat_ms=0
sum_lat_ms=0
last_tip=""
tip_down=0

tmp_body="$(mktemp "${TMPDIR:-/tmp}/soak_body.XXXXXX")"
cleanup_tmp() { rm -f "$tmp_body"; }
trap cleanup_tmp EXIT

record_line() {
  jq -nc --arg run_id "$RUN_ID" --arg kind "$1" --argjson payload "$2" \
    '{ts:(now|floor), run_id:$run_id, kind:$kind, payload:$payload}' >>"$EVENTS"
}

curl_json_meta() {
  local url="$1"
  shift
  # Writes body to tmp_body; prints "HTTP_CODE TIME_TOTAL" to stdout
  curl -sS --max-time 30 -o "$tmp_body" -w "%{http_code} %{time_total}" "$@" "$url"
}

echo "[soak] RUN_ID=$RUN_ID BASE=$base_trim every ${INTERVAL_SEC}s for ${DURATION_SEC}s -> $OUT_DIR"

while (( $(date +%s) < deadline )); do
  iter=$((iter + 1))

  meta="$(curl_json_meta "$base_trim/api/status" || echo "0 0")"
  code="$(echo "$meta" | awk '{print $1}')"
  tt="$(echo "$meta" | awk '{print $2}')"
  lat_ms="$(awk -v t="$tt" 'BEGIN{printf "%.0f", t*1000}')"

  if [[ "$code" == "200" && -s "$tmp_body" ]]; then
    ok_http=$((ok_http + 1))
    [[ "$lat_ms" -gt "$max_lat_ms" ]] && max_lat_ms=$lat_ms
    sum_lat_ms=$((sum_lat_ms + lat_ms))
    body="$(cat "$tmp_body")"
    tip="$(echo "$body" | jq -r '.tip_height // empty')"
    if [[ -n "$last_tip" && -n "$tip" && "$tip" =~ ^[0-9]+$ && "$last_tip" =~ ^[0-9]+$ ]]; then
      if (( tip < last_tip )); then
        tip_down=$((tip_down + 1))
        record_line "tip_regressed" "$(jq -nc --argjson prev "$last_tip" --argjson now "$tip" '{prev:$prev,now:$now}')"
      fi
    fi
    last_tip="$tip"
    payload="$(echo "$body" | jq -c --argjson http "$code" --argjson lat "$lat_ms" \
      '{http:$http,latency_ms:$lat,tip_height:(.tip_height//null),mining:(.mining//null),has_genesis:(.has_genesis//null),version:(.version//"")}')"
    record_line "status_ok" "$payload"
  else
    fail_http=$((fail_http + 1))
    sample="$(head -c 180 "$tmp_body" 2>/dev/null || true)"
    record_line "status_fail" "$(jq -nc --argjson http "$code" --argjson lat "$lat_ms" --arg sample "$sample" '{http:$http,latency_ms:$lat,sample:$sample}')"
  fi

  meta_g="$(curl_json_meta "$base_trim/api/global/metrics" || echo "0 0")"
  cg="$(echo "$meta_g" | awk '{print $1}')"
  tg="$(echo "$meta_g" | awk '{print $2}')"
  lg_ms="$(awk -v t="$tg" 'BEGIN{printf "%.0f", t*1000}')"
  if [[ "$cg" == "200" && -s "$tmp_body" ]]; then
    record_line "global_metrics" "$(cat "$tmp_body" | jq -c --argjson lat "$lg_ms" '{latency_ms:$lat, ok:.ok, tip:.chain.tip_height, target_mod:.work.target_mod}')"
  else
    record_line "global_metrics_fail" "$(jq -nc --argjson http "$cg" --argjson lat "$lg_ms" '{http:$http,latency_ms:$lat}')"
  fi

  if [[ -n "$COORD_URL" ]]; then
    cu="${COORD_URL%/}/api/work/stats?details=0"
    hdr=()
    if [[ -n "$COORD_ADMIN_TOKEN" ]]; then
      hdr=(-H "X-Hackme-Admin-Token: ${COORD_ADMIN_TOKEN}")
    fi
    meta_w="$(curl_json_meta "$cu" "${hdr[@]}" || echo "0 0")"
    wc="$(echo "$meta_w" | awk '{print $1}')"
    if [[ "$wc" == "200" && -s "$tmp_body" ]]; then
      record_line "work_stats" "$(cat "$tmp_body" | jq -c '{workers_count, issued_ranges, submitted_items, target_mod, ok}')"
    else
      record_line "work_stats_fail" "$(jq -nc --argjson http "$wc" '{http:$http}')"
    fi
  fi

  sleep "$INTERVAL_SEC"
done

avg_lat=0
if [[ "$ok_http" -gt 0 ]]; then
  avg_lat=$((sum_lat_ms / ok_http))
fi
{
  echo "run_id=$RUN_ID"
  echo "base=$base_trim"
  echo "coord_url=${COORD_URL:-}"
  echo "duration_sec=$DURATION_SEC interval_sec=$INTERVAL_SEC iterations=$iter"
  echo "status_http_200=$ok_http status_http_fail=$fail_http"
  echo "latency_ms_max=$max_lat_ms latency_ms_avg=$avg_lat"
  echo "tip_height_regressions=$tip_down"
  echo "events_jsonl=$EVENTS"
} | tee "$SUMMARY"

echo "[soak] done -> $SUMMARY"
