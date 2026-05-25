#!/usr/bin/env bash
# Create security-note-01 fuzz campaign with report ready (local desktop node).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN="$(head -n1 "${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}" | tr -d '\r\n')"
CID="${CID:-campaign-security-note-01}"
TITLE="${TITLE:-security-note-01}"
RUNS="${RUNS:-50}"
TASK_ID="${TASK_ID:-order-security-script-push-001}"

if [[ -z "$ADMIN" ]]; then
  echo "[fuzz-bootstrap] missing admin token" >&2
  exit 1
fi

hdr=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json")

echo "[fuzz-bootstrap] create $CID"
curl -fsS -X POST "$BASE/api/fuzz/campaigns" "${hdr[@]}" -d "$(jq -nc \
  --arg id "$CID" --arg title "$TITLE" --arg task "$TASK_ID" --argjson runs "$RUNS" \
  '{id:$id,campaign_type:"fuzz",status:"planned",title:$title,budget_runs:$runs,owner_ref:"hackme:operator",task_id:$task}')" | jq -c '{ok,id:.campaign.id,title:.campaign.title}'

echo "[fuzz-bootstrap] seed runtime + complete"
curl -fsS -X POST "$BASE/api/fuzz/campaigns/$CID/status" "${hdr[@]}" -d '{"status":"running"}' >/dev/null
curl -fsS -X POST "$BASE/api/fuzz/campaigns/$CID/runtime" "${hdr[@]}" -d "$(jq -nc --argjson r "$RUNS" \
  '{status:"running",runs_done:$r,new_edges:12,new_paths:8,unique_crashes:0,time_to_first_crash_sec:0}')" | jq -c '{ok}'
curl -fsS -X POST "$BASE/api/fuzz/campaigns/$CID/status" "${hdr[@]}" -d '{"status":"completed"}' >/dev/null

echo "[fuzz-bootstrap] report"
curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" "$BASE/api/fuzz/campaigns/$CID/report?limit=20&format=json" | jq -c '{ok,verdict,conf:.security_summary.confidence,sample:.security_summary.sample_size}'

echo "[fuzz-bootstrap] done — in UI: Fuzz → Refresh → select title $TITLE → Open report"
