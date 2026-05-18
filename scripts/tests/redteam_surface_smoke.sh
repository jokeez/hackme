#!/usr/bin/env bash
set -euo pipefail

# Lightweight adversarial smoke checks for public API surface.
# Non-destructive: verifies that privileged endpoints reject unauthenticated requests.
#
# Usage:
#   BASE=http://127.0.0.1:8080 scripts/tests/redteam_surface_smoke.sh

BASE="${BASE:-http://127.0.0.1:8080}"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

check_reject() {
  local name="$1"
  local method="$2"
  local path="$3"
  local data="${4:-}"
  local accepted_codes="${5:-401,403}"
  local body_file="${tmp_dir}/${name}.json"
  local code
  if [[ -n "${data}" ]]; then
    code="$(curl -x "" -sS -o "${body_file}" -w '%{http_code}' -X "${method}" \
      -H "Content-Type: application/json" \
      -d "${data}" \
      "${BASE}${path}" || true)"
  else
    code="$(curl -x "" -sS -o "${body_file}" -w '%{http_code}' -X "${method}" \
      "${BASE}${path}" || true)"
  fi
  if [[ ",${accepted_codes}," == *",${code},"* ]]; then
    echo "[pass] ${name}: rejected with HTTP ${code}"
    return 0
  fi
  echo "[fail] ${name}: expected [${accepted_codes}], got HTTP ${code}" >&2
  if [[ -s "${body_file}" ]]; then
    sed -n '1,4p' "${body_file}" >&2
  fi
  return 1
}

echo "[redteam-smoke] target=${BASE}"
check_reject "mining_start_unauth" "POST" "/api/mining/start"
check_reject "worker_start_unauth" "POST" "/api/worker/start" '{"hashrate_gh_s":1}'
check_reject "hardware_tune_unauth" "POST" "/api/hardware/tune" '{"gpu_index":0,"power_limit_w":120}'
check_reject "sync_run_unauth" "POST" "/api/p2p/sync/run?depth_limit=8&max_apply=8"
check_reject "fuzz_cleanup_unauth" "POST" "/api/fuzz/artifacts/cleanup" '{}'
# Some deployments intentionally do not expose runtime_post endpoint (404).
check_reject "runtime_post_unauth" "POST" "/api/fuzz/runtime/post" '{"campaign_id":"noop"}' "401,403,404"

echo "redteam_surface_smoke: PASS"
