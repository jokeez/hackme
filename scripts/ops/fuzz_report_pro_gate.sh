#!/usr/bin/env bash
# Gate: fuzz_report_v2 customer HTML + pool/local repro artifacts.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
BASE="${BASE:-http://127.0.0.1:8080}"

echo "[fuzz-report-pro] unit tests (pool repro + HTML v2)"
go test -count=1 ./internal/fuzzengine/... ./internal/poolfuzz/... -run 'TestClassifyFinding|TestReproCmdTool|TestPoolFuzzClaimSubmitDetector'
go test -count=1 -run 'TestRenderFuzzReportHTML' .

curl -fsS --max-time 15 "$BASE/api/status" >/dev/null || {
  echo "[fuzz-report-pro] node down at $BASE (skip live HTML)" >&2
  exit 0
}

ADMIN="$(tr -d '\r\n' < "${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}")"
CID="fuzz-report-pro-html-$(date +%s)"
curl -fsS -X POST "$BASE/api/fuzz/campaigns" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "{\"id\":\"$CID\",\"campaign_type\":\"property\",\"status\":\"planned\",\"title\":\"pro html smoke\",\"budget_runs\":8}" >/dev/null

HTML="$(curl -fsS "$BASE/api/fuzz/campaigns/$CID/report?format=html&limit=5" \
  -H "X-Hackme-Admin-Token: $ADMIN")"
echo "$HTML" | grep -q 'fuzz_report_v2' || { echo "missing v2" >&2; exit 1; }
echo "$HTML" | grep -q 'Scope &amp; honesty' || { echo "missing scope" >&2; exit 1; }
echo "[fuzz-report-pro] PASS"
