#!/usr/bin/env bash
# Smoke: developer pages (read-only tracker) + public wallet API — no browser admin.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"
require_cmd curl

BASE="${BASE:-https://hackme.tech}"
failures=0

check_http() {
  local name="$1" url="$2" expect="$3"
  local code
  code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 25 "$url" || echo 000)"
  if [[ "$code" == "$expect" ]]; then
    pass "$name HTTP $code"
  else
    fail_msg "$name expected $expect got $code ($url)"
    failures=$((failures + 1))
  fi
}

check_http "developers.html" "$BASE/developers.html" "200"
check_http "developer-dashboard.html" "$BASE/developer-dashboard.html" "200"
check_http "developer-console.js" "$BASE/assets/developer-console.js" "200"
check_http "developer-dashboard.css" "$BASE/assets/developer-dashboard.css" "200"
check_http "hackme-dev-common.js" "$BASE/assets/hackme-dev-common.js" "200"
check_http "fuzzing-console.html" "$BASE/fuzzing-console.html" "200"
check_http "fuzzing-console.js" "$BASE/assets/fuzzing-console.js" "200"

wallet="$(curl -fsS --max-time 20 "$BASE/api/wallet" 2>/dev/null || echo '{}')"
if echo "$wallet" | jq -e '.public_redacted == true and .do_not_send_hmc == true' >/dev/null 2>&1; then
  pass "GET /api/wallet redacted (no treasury leak)"
elif echo "$wallet" | jq -e '.address' >/dev/null 2>&1; then
  fail_msg "GET /api/wallet still exposes address (deploy node with wallet redaction)"
  failures=$((failures + 1))
else
  pass "GET /api/wallet JSON"
fi

tasks="$(curl -fsS --max-time 20 "$BASE/api/tasks" 2>/dev/null || echo '{}')"
if echo "$tasks" | jq -e '.tasks | type == "array"' >/dev/null 2>&1; then
  pass "GET /api/tasks JSON"
else
  fail_msg "GET /api/tasks invalid"
  failures=$((failures + 1))
fi

pool="$(curl -fsS --max-time 20 "$BASE/pool/coordinator/api/pool/stats" 2>/dev/null || echo '{}')"
if echo "$pool" | jq -e '.status == "ok"' >/dev/null 2>&1; then
  pass "pool stats for console hint"
else
  fail_msg "pool stats"
  failures=$((failures + 1))
fi

if [[ "$failures" -eq 0 ]]; then
  pass "fuzzing developer portal smoke PASS"
else
  fail "fuzzing developer portal smoke FAIL ($failures)"
fi
