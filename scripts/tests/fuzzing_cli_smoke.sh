#!/usr/bin/env bash
# Smoke: hackme-fuzzing CLI binary (register --save after subcommand, wallet, tasks).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

VER="$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_VERSION" 2>/dev/null || true)"
CLI="${FUZZING_CLI:-$ROOT/dist/release_${VER}/hackme-fuzzing-${VER}-linux-amd64}"
BASE="${BASE:-http://127.0.0.1:8080}"
export HACKME_FUZZING_BASE="$BASE"
TOKEN_FILE="${HACKME_DEVELOPER_TOKEN_FILE:-$(mktemp -d)/developer.token}"

[[ -x "$CLI" ]] || fail "fuzzing CLI not found: $CLI (run make_release_bundle or build_fuzzing_cli.sh)"
curl -fsS --max-time 10 "${BASE}/api/status?lite=1" >/dev/null || fail "local node down at $BASE"

export HACKME_DEVELOPER_TOKEN_FILE="$TOKEN_FILE"
rm -f "$TOKEN_FILE"

if ! "$CLI" register --base "$BASE" --save 2>/dev/null | jq -e '.ok == true' >/dev/null; then
  fail "register --save (flags after subcommand) failed"
fi
[[ -s "$TOKEN_FILE" ]] || fail "token not saved to $TOKEN_FILE"

if ! "$CLI" wallet --base "$BASE" 2>/dev/null | jq -e '.address != ""' >/dev/null; then
  fail "wallet after register --save failed"
fi

tasks_json="$("$CLI" tasks --base "$BASE" 2>/dev/null || true)"
if ! echo "$tasks_json" | jq -e '.tasks | type == "array"' >/dev/null 2>&1; then
  fail "tasks list failed (expected JSON with tasks array; node slow? try BASE=http://127.0.0.1:18099)"
fi

pass "fuzzing_cli_smoke PASS ($CLI)"
