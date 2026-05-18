#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

RID="${RUN_ID:-private_stage_gate_$(date -u +%Y%m%dT%H%M%SZ)}"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
DO_FREEZE="${DO_FREEZE:-1}"
DO_BACKUP="${DO_BACKUP:-1}"
REQUIRE_COORD_HEALTH="${REQUIRE_COORD_HEALTH:-0}"
FUZZ_CAMPAIGN_ID="${FUZZ_CAMPAIGN_ID:-}"
FUZZ_REPORT_TOKEN="${FUZZ_REPORT_TOKEN:-}"

OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
OUT="$OUT_DIR/$RID/private_stage_gate"
ensure_reports_dir "$OUT"
RESULTS="$OUT/results.jsonl"
: >"$RESULTS"

token_is_placeholder() {
  local t="$1"
  [[ "$t" == *"..."* || "$t" == *"ТУТ_ПОЛНЫЙ_ТОКЕН"* || "$t" == *"CHANGE_ME"* ]]
}

record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
}

http_json() {
  local url="$1"
  curl_retry_fsS -fsS --max-time "${HTTP_JSON_MAX_TIME:-30}" "$url"
}

echo "[private-gate] RID=$RID BASE=$BASE COORD=$COORD DO_FREEZE=$DO_FREEZE DO_BACKUP=$DO_BACKUP"

if [[ -z "$ADMIN_TOKEN" ]]; then
  fail "[private-gate] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required"
fi
if token_is_placeholder "$ADMIN_TOKEN"; then
  fail "[private-gate] ADMIN_TOKEN looks like placeholder"
fi

status_json="$OUT/status.json"
metrics_json="$OUT/metrics.json"
peers_json="$OUT/p2p_peers.json"
sync_json="$OUT/p2p_sync.json"
coord_health_json="$OUT/coordinator_health.json"
hardware_report_json="$OUT/hardware_report.json"

http_json "$BASE/api/status" >"$status_json"
http_json "$BASE/api/metrics" >"$metrics_json"
http_json "$BASE/api/p2p/peers" >"$peers_json"
http_json "$BASE/api/p2p/sync?depth_limit=64" >"$sync_json"
http_json "$BASE/api/reports/hardware?format=json" >"$hardware_report_json"
coord_http="$(curl -sS -o "$coord_health_json" -w '%{http_code}' "$COORD/api/network/stats" || true)"
if [[ "$coord_http" == "200" || "$coord_http" == "405" ]]; then
  record "coordinator-health" "pass" "coordinator health endpoint reachable"
else
  if [[ "$REQUIRE_COORD_HEALTH" == "1" ]]; then
    record "coordinator-health" "fail" "coordinator health endpoint unreachable"
  else
    record "coordinator-health" "pass" "coordinator health endpoint unreachable (allowed in single-node mode)"
  fi
fi

schema_ok="$(jq -r '(.schema_version == .schema_expected)' "$status_json")"
if [[ "$schema_ok" == "true" ]]; then
  record "schema-version-match" "pass" "schema_version equals schema_expected"
else
  record "schema-version-match" "fail" "$(jq -c '{schema_version,schema_expected}' "$status_json")"
fi

admin_auth="$(jq -r '.admin_auth_enabled // false' "$status_json")"
if [[ "$admin_auth" == "true" ]]; then
  record "admin-auth-enabled" "pass" "admin auth enabled"
else
  record "admin-auth-enabled" "fail" "admin auth disabled"
fi

sync_has_blocked="$(jq -r '(.enabled == false) or (has("sync_blocked") and has("sync_blocked_code") and has("sync_action"))' "$sync_json")"
if [[ "$sync_has_blocked" == "true" ]]; then
  record "p2p-sync-diagnostics" "pass" "sync diagnostics contract satisfied"
else
  record "p2p-sync-diagnostics" "fail" "missing sync_blocked/sync_blocked_code/sync_action fields"
fi

hardware_has_devices="$(jq -r '((.devices // []) | length) >= 1' "$hardware_report_json")"
if [[ "$hardware_has_devices" == "true" ]]; then
  record "hardware-report-devices" "pass" "hardware report contains devices"
else
  record "hardware-report-devices" "fail" "hardware report has no devices"
fi

if [[ -n "$FUZZ_CAMPAIGN_ID" ]]; then
  fuzz_report_status="$OUT/fuzz_report_status.txt"
  fuzz_report_body="$OUT/fuzz_report_no_token.json"
  curl -sS -o "$fuzz_report_body" -w "%{http_code}" \
    "$BASE/api/fuzz/campaigns/$FUZZ_CAMPAIGN_ID/report?limit=10" >"$fuzz_report_status" || true
  no_token_code="$(tr -d '[:space:]' <"$fuzz_report_status")"
  if [[ "$no_token_code" == "401" ]]; then
    record "fuzz-report-auth-no-token" "pass" "report endpoint correctly rejects anonymous access"
  else
    record "fuzz-report-auth-no-token" "fail" "expected HTTP 401 without token, got ${no_token_code:-unknown}"
  fi

  if [[ -n "$FUZZ_REPORT_TOKEN" ]]; then
    fuzz_report_with_token="$OUT/fuzz_report_with_token.json"
    if curl -fsS -H "X-Hackme-Report-Token: $FUZZ_REPORT_TOKEN" \
      "$BASE/api/fuzz/campaigns/$FUZZ_CAMPAIGN_ID/report?limit=10" >"$fuzz_report_with_token"; then
      if jq -e '.ok == true and .report_version == "fuzz_report_v1"' "$fuzz_report_with_token" >/dev/null; then
        record "fuzz-report-auth-with-token" "pass" "report endpoint accessible with customer token"
      else
        record "fuzz-report-auth-with-token" "fail" "token response is not valid fuzz report"
      fi
    else
      record "fuzz-report-auth-with-token" "fail" "report endpoint failed with provided customer token"
    fi
  else
    record "fuzz-report-auth-with-token" "fail" "FUZZ_CAMPAIGN_ID is set but FUZZ_REPORT_TOKEN is empty"
  fi
fi

if [[ "$DO_FREEZE" == "1" ]]; then
  if RUN_ID="$RID" BASE="$BASE" COORD="$COORD" "$ROOT_DIR/scripts/ops/freeze_baseline.sh" >/dev/null 2>&1; then
    record "freeze-baseline" "pass" "baseline snapshot captured"
  else
    record "freeze-baseline" "fail" "freeze_baseline.sh failed"
  fi
fi

if [[ "$DO_BACKUP" == "1" ]]; then
  if "$ROOT_DIR/scripts/ops/backup_db.sh" >/dev/null 2>&1; then
    record "backup-db" "pass" "database backup created"
  else
    record "backup-db" "fail" "backup_db.sh failed"
  fi
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"
total="$(wc -l <"$RESULTS" | tr -d ' ')"

jq -nc \
  --arg run_id "$RID" \
  --arg captured_at "$(ts_utc)" \
  --arg base "$BASE" \
  --arg coord "$COORD" \
  --argjson total "$total" \
  --argjson fails "$fails" \
  '{run_id:$run_id,captured_at:$captured_at,base:$base,coord:$coord,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' \
  >"$OUT/summary.json"

if [[ "$fails" != "0" ]]; then
  fail "[private-gate] FAIL ($fails/$total). See $OUT"
fi
pass "[private-gate] PASS ($total checks). See $OUT"
