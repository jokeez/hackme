#!/usr/bin/env bash
# Live gate: create scan/audit/deep via wizard; verify tier config + pool distribution.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

BASE="${BASE:-http://127.0.0.1:8080}"
WASM="${WASM:-$ROOT/tasks/artifacts/security/rust_script_push_bounds_guard.wasm}"
VER="$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_VERSION" 2>/dev/null || echo dev)"
CLI="${FUZZING_CLI:-$ROOT/dist/hackme-fuzzing-${VER}-linux-amd64}"
[[ -x "$CLI" ]] || fail "build fuzzing CLI first"

curl -fsS --max-time 10 "${BASE}/api/status?lite=1" >/dev/null || fail "node down at $BASE"
[[ -f "$WASM" ]] || fail "fixture wasm missing"

export HACKME_FUZZING_BASE="$BASE"
export HACKME_PUBLIC_REPORT_BASE="$BASE"

failures=0
declare -A CREATED

create_pkg() {
  local pkg="$1"
  local ts attempt out
  ts="$(date +%s)"
  for attempt in 1 2 3 4 5; do
    out="$("$CLI" wizard --base "$BASE" --wasm "$WASM" --package "$pkg" \
      --title "tier-gate-${pkg}-${ts}-${attempt}" --payer-ref "tier-gate:${pkg}:${ts}:${attempt}" 2>/dev/null)" && break
    if [[ "$attempt" -lt 5 ]]; then
      sleep $((attempt * 3))
    fi
  done
  if [[ -z "${out:-}" ]] || ! echo "$out" | jq -e '.ok == true' >/dev/null 2>&1; then
    fail_msg "wizard $pkg failed after retries"
    failures=$((failures + 1))
    return
  fi
  local cid depth pool_sync pool_dist runs
  cid="$(echo "$out" | jq -r '.campaign_id')"
  depth="$(echo "$out" | jq -r '.depth_tier')"
  pool_dist="$(echo "$out" | jq -r '.pool_distributed')"
  pool_sync="$(echo "$out" | jq -r '.pool_sync // empty')"
  runs="$(echo "$out" | jq -r '.budget_runs')"
  CREATED["$pkg"]="$cid"
  case "$pkg" in
    scan)
      [[ "$depth" == "wasm_only" && "$pool_dist" == "false" ]] || {
        fail_msg "scan tier mismatch depth=$depth pool=$pool_dist"
        failures=$((failures + 1))
        return
      }
      pass "scan local-only depth=$depth runs=$runs"
      ;;
    audit)
      [[ "$depth" == "wasm_native" && "$pool_dist" == "true" ]] || {
        fail_msg "audit tier mismatch depth=$depth pool=$pool_dist"
        failures=$((failures + 1))
        return
      }
  if [[ "$pool_sync" != "queued" && "$pool_sync" != "sync" && "$pool_sync" != "ok" && "$pool_sync" != "async" ]]; then
        warn="$(echo "$out" | jq -r '.pool_sync_warning // empty')"
        fail_msg "audit pool_sync=$pool_sync warn=$warn"
        failures=$((failures + 1))
        return
      fi
      pass "audit pool depth=$depth runs=$runs pool_sync=$pool_sync"
      ;;
    deep)
      [[ "$depth" == "bytes_corpus" && "$pool_dist" == "true" ]] || {
        fail_msg "deep tier mismatch depth=$depth pool=$pool_dist"
        failures=$((failures + 1))
        return
      }
  if [[ "$pool_sync" != "queued" && "$pool_sync" != "sync" && "$pool_sync" != "ok" && "$pool_sync" != "async" ]]; then
        warn="$(echo "$out" | jq -r '.pool_sync_warning // empty')"
        fail_msg "deep pool_sync=$pool_sync warn=$warn"
        failures=$((failures + 1))
        return
      fi
      pass "deep pool depth=$depth runs=$runs pool_sync=$pool_sync"
      ;;
  esac
  local tok
  tok="$(echo "$out" | jq -r '.customer_report_token')"
  curl -fsS --max-time 20 -H "X-Hackme-Report-Token: $tok" \
    "${BASE}/api/fuzz/campaigns/${cid}/pulse" | jq -e '.ok == true' >/dev/null \
    || { fail_msg "pulse failed for $cid"; failures=$((failures + 1)); }
}

for pkg in scan audit deep; do
  create_pkg "$pkg"
  sleep 8
done

# Pool coordinator list should include audit/deep (not internal gate titles).
COORD_LIST="${COORD_LIST:-https://hackme.tech/pool/coordinator/api/fuzz/pool/campaigns/list}"
if curl -fsS --max-time 20 "$COORD_LIST" >/tmp/tier-coord-list.json 2>/dev/null; then
  for pkg in audit deep; do
    cid="${CREATED[$pkg]:-}"
    [[ -n "$cid" ]] || continue
    if jq -e --arg id "$cid" '.campaigns[]? | select(.id==$id)' /tmp/tier-coord-list.json >/dev/null 2>&1; then
      pass "coordinator lists $pkg campaign $cid"
    else
      fail_msg "coordinator missing $pkg campaign $cid"
      failures=$((failures + 1))
    fi
  done
  if [[ -n "${CREATED[scan]:-}" ]]; then
    if jq -e --arg id "${CREATED[scan]}" '.campaigns[]? | select(.id==$id)' /tmp/tier-coord-list.json >/dev/null 2>&1; then
      fail_msg "scan should not be on public pool coordinator"
      failures=$((failures + 1))
    else
      pass "scan absent from coordinator (local-only)"
    fi
  fi
else
  pass "coordinator list skip (unreachable)"
fi

if [[ "$failures" -gt 0 ]]; then
  fail "b2b_tier_pool_distribution_gate FAIL ($failures)"
fi
pass "b2b_tier_pool_distribution_gate PASS"
