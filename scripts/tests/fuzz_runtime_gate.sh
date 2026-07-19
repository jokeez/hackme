#!/usr/bin/env bash
set -euo pipefail

# End-to-end smoke gate for fuzz runtime flow.
# Requires running node and ADMIN_TOKEN.
#
# Usage:
#   ADMIN_TOKEN=... BASE=http://127.0.0.1:8080 scripts/tests/fuzz_runtime_gate.sh

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
FUZZ_GATE_CURL_RETRIES="${FUZZ_GATE_CURL_RETRIES:-25}"
FUZZ_GATE_CURL_RETRY_DELAY_SEC="${FUZZ_GATE_CURL_RETRY_DELAY_SEC:-0.35}"

if [[ -z "${ADMIN_TOKEN}" ]]; then
  echo "ADMIN_TOKEN is required" >&2
  exit 2
fi

# Retry transient TCP failures (busy host, backlog right after heavy gates).
curl_api() {
  local attempt=1 out ec
  while [[ "$attempt" -le "$FUZZ_GATE_CURL_RETRIES" ]]; do
    ec=0
    out="$(curl "$@")" || ec=$?
    if [[ "$ec" -eq 0 ]]; then
      printf '%s' "$out"
      return 0
    fi
    if [[ "$ec" -eq 7 || "$ec" -eq 28 ]]; then
      echo "[fuzz_runtime_gate] curl transient failure (exit $ec), attempt $attempt/$FUZZ_GATE_CURL_RETRIES" >&2
      sleep "$FUZZ_GATE_CURL_RETRY_DELAY_SEC"
      attempt=$((attempt + 1))
      continue
    fi
    return "$ec"
  done
  return 1
}

# Second-level uniqueness: date +%s alone collides when the script runs twice in the same second.
cid="campaign-gate-$(date +%s)-$$-${RANDOM}"

echo "[1/8] create campaign: ${cid}"
create_resp="$(curl_api -x "" -sS -X POST \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"id\":\"${cid}\",\"campaign_type\":\"fuzz\",\"status\":\"planned\",\"title\":\"gate-smoke\",\"budget_runs\":10}" \
  "${BASE}/api/fuzz/campaigns")"
echo "${create_resp}" | jq -e '.ok == true' >/dev/null

echo "[2/8] runtime heartbeat update"
rt_resp="$(curl_api -x "" -sS -X POST \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"runs_done":3,"new_edges":12,"new_paths":5,"unique_crashes":1,"time_to_first_crash_sec":14}' \
  "${BASE}/api/fuzz/campaigns/${cid}/runtime")"
echo "${rt_resp}" | jq -e '.ok == true' >/dev/null

echo "[3/8] ingest findings with dedup pressure"
find_resp="$(curl_api -x "" -sS -X POST \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "findings": [
      {"id":"f-a","finding_type":"crash","severity":"high","title":"panic on malformed len","input_sha256":"aaa111","artifact_path":"/tmp/crash-a"},
      {"id":"f-b","finding_type":"crash","severity":"critical","title":"heap overwrite","input_sha256":"bbb222","artifact_path":"/tmp/crash-b"},
      {"id":"f-dup-id","finding_type":"crash","severity":"high","title":"panic on malformed len","input_sha256":"aaa111","artifact_path":"/tmp/crash-a"},
      {"id":"f-dup-id","finding_type":"crash","severity":"high","title":"panic on malformed len","input_sha256":"aaa111","artifact_path":"/tmp/crash-a"}
    ]
  }' \
  "${BASE}/api/fuzz/campaigns/${cid}/findings")"
echo "${find_resp}" | jq -e '.ok == true and (.summary != null)' >/dev/null

echo "[4/8] pulse (admin)"
pulse_resp="$(curl_api -x "" -sS \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  "${BASE}/api/fuzz/campaigns/${cid}/pulse")"
echo "${pulse_resp}" | jq -e '.runner != null and .coverage != null and .runner.queue != null' >/dev/null

echo "[5/8] corpus snapshot"
corpus_resp="$(curl_api -x "" -sS \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  "${BASE}/api/fuzz/campaigns/${cid}/corpus?limit=20")"
echo "${corpus_resp}" | jq -e '.ok == true and (.corpus | type) == "array"' >/dev/null

echo "[6/8] crashes view"
crash_resp="$(curl_api -x "" -sS \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  "${BASE}/api/fuzz/campaigns/${cid}/crashes?limit=20")"
echo "${crash_resp}" | jq -e '.ok == true and (.crashes | type) == "array"' >/dev/null

echo "[7/8] access summary"
acc_resp="$(curl_api -x "" -sS \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  "${BASE}/api/fuzz/campaigns/${cid}/access/summary?window_sec=86400")"
echo "${acc_resp}" | jq -e '.ok == true and .by_access_kind != null' >/dev/null

echo "[8/9] runtime history"
hist_resp="$(curl_api -x "" -sS \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  "${BASE}/api/fuzz/campaigns/${cid}/runtime/history?limit=20")"
echo "${hist_resp}" | jq -e '.ok == true and (.samples | type) == "array"' >/dev/null

echo "[9/9] gate"
gate_resp="$(curl_api -x "" -sS \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  "${BASE}/api/fuzz/campaigns/${cid}/gate?max_critical=0&max_high=5&max_severity_score=500&min_sample_size=1")"
echo "${gate_resp}" | jq -e '.ok == true and (.pass != null) and ((.assurance_note // "") | length) > 0 and (.observed.sample_size != null)' >/dev/null

echo "fuzz_runtime_gate: PASS (campaign_id=${cid})"
