#!/usr/bin/env bash
# Smoke: hackme-fuzzing wizard refuses public base; scan package dry payload on local node.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

ROOT="$ROOT" go test ./cmd/fuzzingclient/ -run TestWizard -count=1 -timeout=30s

VER="$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_VERSION" 2>/dev/null || echo dev)"
CLI="${FUZZING_CLI:-$ROOT/dist/hackme-fuzzing-${VER}-linux-amd64}"
[[ -x "$CLI" ]] || CLI="$ROOT/dist/hackme-fuzzing-dev-linux-amd64"
[[ -x "$CLI" ]] || fail "fuzzing CLI missing (run build_fuzzing_client.sh)"

WASM="$ROOT/tasks/artifacts/security/rust_script_push_bounds_guard.wasm"
[[ -f "$WASM" ]] || fail "fixture wasm missing"

if "$CLI" wizard --base https://hackme.tech --wasm "$WASM" 2>/dev/null; then
  fail "wizard must refuse hackme.tech base"
fi
pass "wizard blocks public base"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN="$(resolve_admin_token "$ROOT")"
[[ -n "$ADMIN" ]] || fail "missing admin token (ADMIN_TOKEN, .env.desktop, or .secrets/hackme_admin_token)"
curl -fsS --max-time 10 "${BASE}/api/status?lite=1" >/dev/null || fail "node down at $BASE"

hdr=(-H "X-Hackme-Admin-Token: $ADMIN")
bal="$(curl -fsS --max-time 15 "${hdr[@]}" "${BASE}/api/wallet" | jq -r '.balance_orders_spendable_hmc // .balance_hmc // 0')"
if awk -v b="$bal" 'BEGIN{exit !(b+0 < 0.5)}'; then
  curl -fsS --max-time 30 -X POST "${hdr[@]}" "${BASE}/api/genesis" -d '{}' >/dev/null 2>&1 || true
  bal="$(curl -fsS --max-time 15 "${hdr[@]}" "${BASE}/api/wallet" | jq -r '.balance_orders_spendable_hmc // .balance_hmc // 0')"
fi
if awk -v b="$bal" 'BEGIN{exit !(b+0 < 0.5)}'; then
  pass "fuzz_b2b_wizard_smoke SKIP live audit (wallet spendable ${bal} HMC < 0.5; public-base guard OK)"
  exit 0
fi

export HACKME_ADMIN_TOKEN="$ADMIN"
export HACKME_PUBLIC_REPORT_BASE="http://127.0.0.1:8080"
out="$("$CLI" wizard --base "$BASE" --wasm "$WASM" --package scan --title "b2b-wizard-smoke" --payer-ref "gate:wizard-smoke" 2>/dev/null)"
echo "$out" | jq -e '.ok == true and .campaign_id != "" and .customer_report_token != ""' >/dev/null
CID="$(echo "$out" | jq -r '.campaign_id')"
TOK="$(echo "$out" | jq -r '.customer_report_token')"
curl -fsS --max-time 30 -H "X-Hackme-Report-Token: $TOK" \
  "${BASE}/api/fuzz/campaigns/${CID}/pulse" | jq -e '.ok == true' >/dev/null

pass "fuzz_b2b_wizard_smoke PASS (campaign=$CID)"
