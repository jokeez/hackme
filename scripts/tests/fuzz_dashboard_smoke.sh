#!/usr/bin/env bash
# Smoke-test fuzz campaign API paths used by dashboard buttons.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
if [[ -z "$ADMIN_TOKEN" && -f "$ROOT_DIR/.env.desktop" ]]; then
  ADMIN_TOKEN="$(grep '^HACKME_ADMIN_TOKEN=' "$ROOT_DIR/.env.desktop" | cut -d= -f2- | tr -d '\r\n')"
fi
if [[ -z "$ADMIN_TOKEN" ]]; then
  fail "ADMIN_TOKEN required (or .env.desktop HACKME_ADMIN_TOKEN)"
fi

HDR_AUTH=(-H "X-Hackme-Admin-Token: $ADMIN_TOKEN")
HDR_JSON=(-H "X-Hackme-Admin-Token: $ADMIN_TOKEN" -H "Content-Type: application/json")
OUT="${OUT_DIR:-$ROOT_DIR/reports/tests}/fuzz-smoke-$(date +%Y%m%dT%H%M%S)"
mkdir -p "$OUT"
log() { echo "[fuzz-smoke] $*" | tee -a "$OUT/run.log"; }

http_code() {
  local outfile=$1 method=$2 url=$3
  shift 3
  curl -sS -o "$outfile" -w '%{http_code}' -X "$method" "$url" "$@"
}

log "BASE=$BASE"

# list
code="$(http_code "$OUT/list.json" GET "$BASE/api/fuzz/campaigns?limit=20")"
[[ "$code" == "200" ]] || fail "list HTTP $code"
HEAD_ID="$(jq -r '.campaigns[0].id // empty' "$OUT/list.json")"
[[ -n "$HEAD_ID" ]] || fail "no campaigns in DB"
log "head campaign: $HEAD_ID"

# create
NEW_ID="campaign-smoke-$(date +%s)"
code="$(http_code "$OUT/create.json" POST "$BASE/api/fuzz/campaigns" \
  "${HDR_JSON[@]}" \
  -d "{\"id\":\"$NEW_ID\",\"campaign_type\":\"fuzz\",\"status\":\"planned\",\"title\":\"smoke\",\"budget_runs\":42}")"
[[ "$code" == "200" || "$code" == "201" ]] || fail "create HTTP $code"
log "created $NEW_ID"

CID="$NEW_ID"
for path in \
  "GET:/api/fuzz/campaigns/$CID/pulse" \
  "GET:/api/fuzz/campaigns/$CID/access/summary?window_sec=3600" \
  "GET:/api/fuzz/campaigns/$CID/runtime/history?limit=10" \
  "GET:/api/fuzz/campaigns/$CID/crashes?limit=5" \
  "GET:/api/fuzz/campaigns/$CID/corpus?limit=5" \
  "GET:/api/fuzz/campaigns/$CID/report?limit=10&format=json" \
  "GET:/api/fuzz/campaigns/$CID/gate?max_critical=0&max_high=5" \
  "GET:/api/fuzz/campaigns/$CID/diff?base_campaign_id=$HEAD_ID"
do
  meth="${path%%:*}"
  url="${path#*:}"
  tmp="$OUT/$(echo "$url" | tr '/?&=' '_').json"
  code="$(http_code "$tmp" "$meth" "$BASE$url" "${HDR_AUTH[@]}")"
  log "$meth $url -> HTTP $code"
  [[ "$code" == "200" ]] || fail "$url HTTP $code"
done

# status + runtime POST
code="$(http_code "$OUT/status.json" POST "$BASE/api/fuzz/campaigns/$CID/status" \
  "${HDR_JSON[@]}" -d '{"status":"running"}')"
[[ "$code" == "200" ]] || fail "status POST HTTP $code"

code="$(http_code "$OUT/runtime.json" POST "$BASE/api/fuzz/campaigns/$CID/runtime" \
  "${HDR_JSON[@]}" -d '{"runs_done":1,"new_edges":2,"new_paths":1,"unique_crashes":0,"time_to_first_crash_sec":0}')"
[[ "$code" == "200" ]] || fail "runtime POST HTTP $code"

code="$(http_code "$OUT/retain.json" POST "$BASE/api/fuzz/campaigns/$CID/corpus/retention" \
  "${HDR_JSON[@]}" -d '{"max_items":1000}')"
[[ "$code" == "200" ]] || fail "retention HTTP $code"

code="$(http_code "$OUT/hk.json" POST "$BASE/api/fuzz/campaigns/$CID/housekeeping" \
  "${HDR_JSON[@]}" -d '{"max_findings":100,"max_corpus":100,"max_runtime_samples":100}')"
[[ "$code" == "200" ]] || fail "housekeeping HTTP $code"

code="$(http_code "$OUT/hk_global.json" POST "$BASE/api/fuzz/housekeeping" \
  "${HDR_JSON[@]}" -d '{"max_findings":100,"max_corpus":100,"max_runtime_samples":100}')"
[[ "$code" == "200" ]] || fail "global housekeeping HTTP $code"

html_tmp="$OUT/report_html.body"
code="$(http_code "$html_tmp" GET "$BASE/api/fuzz/campaigns/$CID/report.html?limit=5" "${HDR_AUTH[@]}")"
[[ "$code" == "200" ]] || fail "report.html HTTP $code"
grep -q '<!DOCTYPE html>' "$html_tmp" || fail "report.html missing doctype"
grep -q 'HackMe Security Report' "$html_tmp" || fail "report.html missing title"
grep -q 'fuzz_report_v2' "$html_tmp" || fail "report.html missing fuzz_report_v2"
log "report.html OK ($(wc -c <"$html_tmp") bytes)"

pass "fuzz dashboard API smoke PASS — see $OUT"
