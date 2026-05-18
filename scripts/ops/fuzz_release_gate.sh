#!/usr/bin/env bash
set -euo pipefail

# Final release gate for fuzz campaign stack.
# Requires running node and ADMIN_TOKEN.
# Does not run manifest/WASM static checks; for an isolated gate run those first, e.g.
#   MODE=lang_static bash scripts/tests/run_daily.sh
# or STATIC_ONLY=1 scripts/tests/run_language_production_pack.sh
#
# Usage (from repo root):
#   ADMIN_TOKEN=... BASE=http://127.0.0.1:8080 bash scripts/ops/fuzz_release_gate.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
RUN_REDTEAM_SMOKE="${RUN_REDTEAM_SMOKE:-1}"
RUN_LANGUAGE_MATRIX="${RUN_LANGUAGE_MATRIX:-1}"
RUN_ORDERS_MULTILANG_AUDIT="${RUN_ORDERS_MULTILANG_AUDIT:-1}"
RUN_LANGUAGE_BREAK_ATTEMPTS="${RUN_LANGUAGE_BREAK_ATTEMPTS:-1}"
RUN_CHAOS_LANG_SECURITY="${RUN_CHAOS_LANG_SECURITY:-1}"

if [[ -z "${ADMIN_TOKEN}" ]]; then
  echo "ADMIN_TOKEN is required" >&2
  exit 2
fi

echo "[1/13] status contract (economics + policy hash)"
status_resp="$(curl_retry_fsS -x "" -sS "${BASE}/api/status")"
# economics can be omitted/null in follower/network overlay mode; validate strictly when present.
if echo "${status_resp}" | jq -e '.economics != null' >/dev/null; then
  echo "${status_resp}" | jq -e '.economics.order_fee_rate != null' >/dev/null
  echo "${status_resp}" | jq -e '.economics.network_fee_burn_share != null' >/dev/null
  echo "${status_resp}" | jq -e '.economics.network_fee_dev_share != null' >/dev/null
  echo "${status_resp}" | jq -e '(.economics.dev_fee_address | type) == "string" and (.economics.dev_fee_address | length) > 0' >/dev/null
  echo "${status_resp}" | jq -e '(.economics.policy_hash | type) == "string" and (.economics.policy_hash | length) == 64 and ((.economics.policy_hash | test("^[0-9a-fA-F]{64}$")) == true)' >/dev/null
else
  echo "[warn] status.economics is null (network/follower mode); economics contract skipped in this gate run"
fi
echo "${status_resp}" | jq -e '.sandbox_policy.locked == true' >/dev/null
echo "${status_resp}" | jq -e '.sandbox_policy.profile == "secure"' >/dev/null
echo "${status_resp}" | jq -e '.sandbox_policy.block_quarantined == true' >/dev/null

echo "[2/13] campaign listing + pulse endpoint availability"
list_resp="$(curl_retry_fsS -x "" -sS "${BASE}/api/fuzz/campaigns?limit=5")"
echo "${list_resp}" | jq -e '.ok == true and (.campaigns | type) == "array"' >/dev/null

echo "[3/13] runtime gate flow"
ADMIN_TOKEN="${ADMIN_TOKEN}" BASE="${BASE}" bash scripts/tests/fuzz_runtime_gate.sh

echo "[4/13] mining/difficulty contract"
metrics_resp="$(curl_retry_fsS -x "" -sS "${BASE}/api/metrics")"
echo "${metrics_resp}" | jq -e '.mining_target_mod >= 251 and .mining_target_mod <= 10000000000000' >/dev/null
echo "${metrics_resp}" | jq -e '.mining_target_block_sec == 30' >/dev/null

echo "[5/13] p2p peers endpoint availability"
peers_resp="$(curl_retry_fsS -x "" -sS "${BASE}/api/p2p/peers")"
echo "${peers_resp}" | jq -e 'has("enabled") and ((.peers | type) == "array")' >/dev/null

echo "[6/13] p2p sync contract"
sync_resp="$(curl_retry_fsS -x "" -sS "${BASE}/api/p2p/sync?depth_limit=8")"
echo "${sync_resp}" | jq -e '(.enabled == false) or (has("sync_blocked") and has("sync_blocked_code") and has("sync_action"))' >/dev/null
sync_run_resp="$(curl_retry_fsS -x "" -sS -X POST \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  "${BASE}/api/p2p/sync/run?depth_limit=8&max_apply=8")"
echo "${sync_run_resp}" | jq -e '(.code // "") == "sync_apply_disabled_no_state_replay_v1" or (.code // "") == "fork_detected_no_reorg_v1" or (.code // "") == "plan_not_ready" or (.code // "") == "p2p_disabled" or (.apply.reason // "") == "ok" or (.apply.reason // "") == "empty_stage"' >/dev/null

echo "[7/13] artifact cleanup endpoint availability"
artifacts_resp="$(curl_retry_fsS -x "" -sS -X POST \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' \
  "${BASE}/api/fuzz/artifacts/cleanup")"
echo "${artifacts_resp}" | jq -e '.ok == true and (.artifacts | type) == "object"' >/dev/null

echo "[8/13] language from_code matrix"
if [[ "${RUN_LANGUAGE_MATRIX}" == "1" ]]; then
  ADMIN_TOKEN="${ADMIN_TOKEN}" BASE="${BASE}" bash scripts/tests/language_from_code_matrix.sh
else
  echo "language matrix skipped (RUN_LANGUAGE_MATRIX=0)"
fi

echo "[9/13] orders multilang audit"
if [[ "${RUN_ORDERS_MULTILANG_AUDIT}" == "1" ]]; then
  ADMIN_TOKEN="${ADMIN_TOKEN}" BASE="${BASE}" bash scripts/tests/orders_multilang_audit.sh
else
  echo "orders multilang audit skipped (RUN_ORDERS_MULTILANG_AUDIT=0)"
fi

echo "[10/13] language break attempts"
if [[ "${RUN_LANGUAGE_BREAK_ATTEMPTS}" == "1" ]]; then
  ADMIN_TOKEN="${ADMIN_TOKEN}" BASE="${BASE}" bash scripts/tests/language_break_attempts.sh
else
  echo "language break attempts skipped (RUN_LANGUAGE_BREAK_ATTEMPTS=0)"
fi

echo "[11/13] language chaos security"
if [[ "${RUN_CHAOS_LANG_SECURITY}" == "1" ]]; then
  ADMIN_TOKEN="${ADMIN_TOKEN}" BASE="${BASE}" bash scripts/tests/language_chaos_security.sh
else
  echo "language chaos security skipped (RUN_CHAOS_LANG_SECURITY=0)"
fi

echo "[12/13] private stage gate (best effort)"
if [[ -f "scripts/ops/private_stage_gate.sh" ]]; then
  if ! ADMIN_TOKEN="${ADMIN_TOKEN}" BASE="${BASE}" bash scripts/ops/private_stage_gate.sh; then
    echo "private_stage_gate: WARN (non-fuzz check failed, inspect output)" >&2
  fi
fi

echo "[13/13] red-team surface smoke (non-destructive)"
if [[ "${RUN_REDTEAM_SMOKE}" == "1" ]]; then
  BASE="${BASE}" bash scripts/tests/redteam_surface_smoke.sh
else
  echo "red-team smoke skipped (RUN_REDTEAM_SMOKE=0)"
fi

echo "fuzz_release_gate: PASS"
