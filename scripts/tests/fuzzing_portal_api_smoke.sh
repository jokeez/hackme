#!/usr/bin/env bash
# API-only slice of developer portal smoke (local node; no static site on :8080).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"
require_cmd curl
require_cmd jq

BASE="${BASE:-http://127.0.0.1:8080}"
failures=0

wallet="$(curl -fsS --max-time 20 "$BASE/api/wallet" 2>/dev/null || echo '{}')"
if echo "$wallet" | jq -e '.public_redacted == true' >/dev/null 2>&1; then
  pass "GET /api/wallet redacted"
elif echo "$wallet" | jq -e '.address' >/dev/null 2>&1; then
  pass "GET /api/wallet local node (admin view)"
else
  fail_msg "GET /api/wallet invalid"
  failures=$((failures + 1))
fi

tasks="$(curl -fsS --max-time 20 "$BASE/api/tasks" 2>/dev/null || echo '{}')"
if echo "$tasks" | jq -e '.tasks | type == "array"' >/dev/null 2>&1; then
  pass "GET /api/tasks JSON"
else
  fail_msg "GET /api/tasks invalid"
  failures=$((failures + 1))
fi

code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 15 -X POST "$BASE/api/tasks/from_code" || echo 000)"
if [[ "$code" == "401" || "$code" == "403" ]]; then
  pass "POST /api/tasks/from_code blocked HTTP $code"
else
  fail_msg "from_code want 401/403 got $code"
  failures=$((failures + 1))
fi

if [[ "$failures" -eq 0 ]]; then
  pass "fuzzing_portal_api_smoke PASS"
else
  fail "fuzzing_portal_api_smoke FAIL ($failures)"
fi
