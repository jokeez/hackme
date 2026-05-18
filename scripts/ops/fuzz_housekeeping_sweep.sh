#!/usr/bin/env bash
set -euo pipefail

# Global fuzz housekeeping sweep across all campaigns.
#
# Usage:
#   ADMIN_TOKEN=... BASE=http://127.0.0.1:8080 scripts/ops/fuzz_housekeeping_sweep.sh
# Optional limits:
#   MAX_FINDINGS=5000 MAX_CORPUS=2000 MAX_RUNTIME_SAMPLES=2000
#   ARTIFACT_TTL_SEC=604800 ARTIFACT_MAX_BYTES=536870912

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
MAX_FINDINGS="${MAX_FINDINGS:-5000}"
MAX_CORPUS="${MAX_CORPUS:-2000}"
MAX_RUNTIME_SAMPLES="${MAX_RUNTIME_SAMPLES:-2000}"
ARTIFACT_TTL_SEC="${ARTIFACT_TTL_SEC:-604800}"
ARTIFACT_MAX_BYTES="${ARTIFACT_MAX_BYTES:-536870912}"

if [[ -z "${ADMIN_TOKEN}" ]]; then
  echo "ADMIN_TOKEN is required" >&2
  exit 2
fi

echo "running fuzz housekeeping sweep..."
resp="$(curl -x "" -sS -X POST \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"max_findings\":${MAX_FINDINGS},\"max_corpus\":${MAX_CORPUS},\"max_runtime_samples\":${MAX_RUNTIME_SAMPLES}}" \
  "${BASE}/api/fuzz/housekeeping")"

echo "${resp}" | jq -e '.ok == true' >/dev/null
echo "${resp}"

echo "running standalone artifact cleanup endpoint..."
artifacts_resp="$(curl -x "" -sS -X POST \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"ttl_sec\":${ARTIFACT_TTL_SEC},\"max_bytes\":${ARTIFACT_MAX_BYTES}}" \
  "${BASE}/api/fuzz/artifacts/cleanup")"
echo "${artifacts_resp}" | jq -e '.ok == true and (.artifacts | type) == "object"' >/dev/null
echo "${artifacts_resp}"
echo "fuzz_housekeeping_sweep: PASS"
