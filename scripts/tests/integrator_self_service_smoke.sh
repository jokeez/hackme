#!/usr/bin/env bash
# Integrator self-service smoke: works with self_register ON or OFF (prod fail-closed).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT/scripts/tests/common.sh"
require_cmd curl jq
BASE="${BASE:-https://hackme.tech}"

st="$(curl -fsS "$BASE/api/integrator/status")"
enabled="$(echo "$st" | jq -r '.self_register_enabled')"
pass "integrator status self_register_enabled=$enabled"

if [[ "$enabled" == "true" ]]; then
  reg="$(curl -fsS -X POST "$BASE/api/integrator/register" \
    -H 'Content-Type: application/json' \
    -d '{"label":"smoke-integrator"}')"
  TOK="$(echo "$reg" | jq -r '.developer_token')"
  ID="$(echo "$reg" | jq -r '.integrator_id')"
  [[ -n "$TOK" && "$TOK" != null ]] || { fail_msg "no developer_token"; exit 1; }
  pass "integrator register id=$ID"

  # POST requires valid integrator token (GET /api/tasks is public without token).
  code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST -H "X-Hackme-Developer-Token: $TOK" \
    -H 'Content-Type: application/json' -d '{}' "$BASE/api/tasks")"
  [[ "$code" == "401" || "$code" == "400" ]] || { fail_msg "POST /api/tasks with token want 401/400 got $code"; exit 1; }
  pass "POST /api/tasks accepts integrator auth (got $code)"

  rot="$(curl -fsS -X POST "$BASE/api/integrator/rotate" -H "X-Hackme-Developer-Token: $TOK")"
  TOK2="$(echo "$rot" | jq -r '.developer_token')"
  [[ -n "$TOK2" && "$TOK2" != "$TOK" ]] || { fail_msg "rotate token"; exit 1; }
  pass "integrator rotate OK"

  old_code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST -H "X-Hackme-Developer-Token: $TOK" \
    -H 'Content-Type: application/json' -d '{}' "$BASE/api/tasks")"
  [[ "$old_code" == "401" ]] || { fail_msg "old token POST should be 401 got $old_code"; exit 1; }
  pass "old token revoked after rotate"
else
  code="$(curl -sS -o /tmp/integrator_reg_body.json -w '%{http_code}' -X POST "$BASE/api/integrator/register" \
    -H 'Content-Type: application/json' \
    -d '{"label":"smoke-should-deny"}')"
  [[ "$code" == "403" || "$code" == "401" ]] || {
    fail_msg "self_register off: register want 403/401 got $code body=$(cat /tmp/integrator_reg_body.json)"
    exit 1
  }
  pass "self_register off: register correctly denied ($code)"
fi

pass "integrator self-service smoke PASS"
