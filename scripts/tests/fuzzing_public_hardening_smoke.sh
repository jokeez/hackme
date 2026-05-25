#!/usr/bin/env bash
# Public hackme.tech must block fuzz/from_code; wallet/tasks read stay open.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT/scripts/tests/common.sh"
require_cmd curl
BASE="${BASE:-https://hackme.tech}"
failures=0

expect_code() {
  local name="$1" url="$2" method="${3:-GET}" want="$4"
  local code
  code="$(curl -sS -o /dev/null -w '%{http_code}' -X "$method" --max-time 20 "$url" || echo 000)"
  if [[ "$code" == "$want" ]]; then
    pass "$name HTTP $code"
  else
    fail_msg "$name want $want got $code"
    failures=$((failures + 1))
  fi
}

expect_code "GET /api/wallet" "$BASE/api/wallet" GET 200
expect_code "GET /api/tasks" "$BASE/api/tasks" GET 200
fc="$(curl -sS -o /dev/null -w '%{http_code}' -X POST --max-time 20 "$BASE/api/tasks/from_code" || echo 000)"
if [[ "$fc" == "403" || "$fc" == "401" ]]; then
  pass "POST /api/tasks/from_code blocked HTTP $fc"
else
  fail_msg "from_code want 401/403 got $fc"
  failures=$((failures + 1))
fi
fz="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 20 "$BASE/api/fuzz/campaigns" || echo 000)"
if [[ "$fz" == "403" ]]; then
  pass "GET /api/fuzz blocked HTTP $fz"
elif [[ "$fz" == "401" ]]; then
  pass "GET /api/fuzz needs auth HTTP $fz (nginx hardening pending)"
else
  fail_msg "fuzz want 403/401 got $fz (deploy SYNC_NGINX_SITE_CONF=1)"
  failures=$((failures + 1))
fi
code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d '{}' --max-time 20 "$BASE/api/tasks" || echo 000)"
if [[ "$code" == "401" || "$code" == "503" ]]; then
  pass "POST /api/tasks denied without token HTTP $code"
else
  fail_msg "POST /api/tasks without token want 401/503 got $code"
  failures=$((failures + 1))
fi

if [[ "$failures" -eq 0 ]]; then
  pass "fuzzing public hardening smoke PASS"
else
  fail "fuzzing public hardening smoke FAIL ($failures)"
fi
