#!/usr/bin/env bash
set -euo pipefail

# Internet preflight gate:
# - validates security/economics/sandbox contracts
# - checks difficulty health and public-surface HTTP hardening
# - verifies p2p/sync/coordinator readiness for public exposure
#
# Usage:
#   ADMIN_TOKEN=... BASE=http://127.0.0.1:8080 COORD=http://127.0.0.1:8081 scripts/ops/internet_preflight.sh
#
# Optional strictness knobs:
#   REQUIRE_P2P=1 MIN_HEALTHY_PEERS=1 MAX_SYNC_LAG_BLOCKS=3 REQUIRE_COORD_HEALTH=1
#   RUN_PRIVATE_STAGE=1 RUN_DIFFICULTY_HEALTH=1
# Optional explorer check:
#   EXPLORER_URL=https://explorer.example.com

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[internet-preflight] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq

BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
RUN_ID="${RUN_ID:-internet_preflight_$(date -u +%Y%m%dT%H%M%SZ)}"

REQUIRE_P2P="${REQUIRE_P2P:-1}"
MIN_HEALTHY_PEERS="${MIN_HEALTHY_PEERS:-1}"
MAX_SYNC_LAG_BLOCKS="${MAX_SYNC_LAG_BLOCKS:-3}"
REQUIRE_COORD_HEALTH="${REQUIRE_COORD_HEALTH:-1}"
RUN_PRIVATE_STAGE="${RUN_PRIVATE_STAGE:-1}"
RUN_DIFFICULTY_HEALTH="${RUN_DIFFICULTY_HEALTH:-1}"
EXPLORER_URL="${EXPLORER_URL:-}"

if [[ -z "${ADMIN_TOKEN}" ]]; then
  echo "[internet-preflight] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 2
fi

out_dir="reports/gates/${RUN_ID}"
mkdir -p "${out_dir}"
results="${out_dir}/results.jsonl"
: >"${results}"

record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"${results}"
}

status_json="${out_dir}/status.json"
metrics_json="${out_dir}/metrics.json"
peers_json="${out_dir}/p2p_peers.json"
sync_json="${out_dir}/p2p_sync.json"
headers_txt="${out_dir}/headers.txt"

curl_retry_fsS -x "" -fsS "${BASE}/api/status" >"${status_json}"
curl_retry_fsS -x "" -fsS "${BASE}/api/metrics" >"${metrics_json}"
curl_retry_fsS -x "" -fsS "${BASE}/api/p2p/peers" >"${peers_json}"
curl_retry_fsS -x "" -fsS "${BASE}/api/p2p/sync?depth_limit=64" >"${sync_json}"
curl_retry_fsS -x "" -sS -D "${headers_txt}" -o /dev/null "${BASE}/api/status"

if jq -e '.has_genesis == true and (.schema_version == .schema_expected)' "${status_json}" >/dev/null; then
  record "status-schema-genesis" "pass" "genesis present and schema matches"
else
  record "status-schema-genesis" "fail" "genesis/schema contract mismatch"
fi

if jq -e '.admin_auth_enabled == true' "${status_json}" >/dev/null; then
  record "admin-auth" "pass" "admin auth enabled"
else
  record "admin-auth" "fail" "admin auth is disabled"
fi

if jq -e '.sandbox_policy.locked == true and .sandbox_policy.profile == "secure" and .sandbox_policy.block_quarantined == true' "${status_json}" >/dev/null; then
  record "sandbox-policy" "pass" "sandbox locked profile secure"
else
  record "sandbox-policy" "fail" "sandbox policy not in secure locked mode"
fi

if jq -e '(.economics.policy_hash | type) == "string" and (.economics.policy_hash | length) > 0' "${status_json}" >/dev/null; then
  record "economics-policy-hash" "pass" "policy hash present"
else
  record "economics-policy-hash" "fail" "policy hash missing"
fi

if jq -e '.mining_target_mod >= 251 and .mining_target_mod <= 10000000000000 and .mining_target_block_sec == 30' "${metrics_json}" >/dev/null; then
  record "difficulty-contract" "pass" "target_mod and target_block_sec are valid"
else
  record "difficulty-contract" "fail" "difficulty metrics out of expected contract"
fi

required_headers_ok=true
lower_headers="$(tr '[:upper:]' '[:lower:]' <"${headers_txt}")"
for h in "x-content-type-options: nosniff" "x-frame-options: DENY" "referrer-policy: no-referrer" "permissions-policy:" "cross-origin-resource-policy: same-origin" "cache-control: no-store"; do
  needle="$(printf '%s' "$h" | tr '[:upper:]' '[:lower:]')"
  if [[ "${lower_headers}" != *"${needle}"* ]]; then
    required_headers_ok=false
  fi
done
if [[ "${required_headers_ok}" == "true" ]]; then
  record "http-hardening-headers" "pass" "security/cache headers present on api/status"
else
  record "http-hardening-headers" "fail" "one or more required hardening headers missing"
fi

p2p_enabled="$(jq -r '.enabled // false' "${peers_json}")"
healthy_count="$(jq -r '[.peers[]? | select(.healthy == true)] | length' "${peers_json}")"
if [[ "${REQUIRE_P2P}" == "1" ]]; then
  if [[ "${p2p_enabled}" == "true" && "${healthy_count}" -ge "${MIN_HEALTHY_PEERS}" ]]; then
    record "p2p-readiness" "pass" "p2p enabled with healthy peers >= ${MIN_HEALTHY_PEERS}"
  else
    record "p2p-readiness" "fail" "p2p not ready: enabled=${p2p_enabled}, healthy=${healthy_count}, required=${MIN_HEALTHY_PEERS}"
  fi
else
  record "p2p-readiness" "pass" "p2p readiness check relaxed (REQUIRE_P2P=0)"
fi

sync_enabled="$(jq -r '.enabled // false' "${sync_json}")"
sync_blocked="$(jq -r '.sync_blocked // false' "${sync_json}")"
sync_lag="$(jq -r '.lag_blocks // 0' "${sync_json}")"
if [[ "${REQUIRE_P2P}" == "1" ]]; then
  if [[ "${sync_enabled}" == "true" && "${sync_blocked}" != "true" && "${sync_lag}" -le "${MAX_SYNC_LAG_BLOCKS}" ]]; then
    record "p2p-sync-readiness" "pass" "sync enabled/unblocked and lag <= ${MAX_SYNC_LAG_BLOCKS}"
  else
    record "p2p-sync-readiness" "fail" "sync not ready: enabled=${sync_enabled}, blocked=${sync_blocked}, lag=${sync_lag}"
  fi
else
  record "p2p-sync-readiness" "pass" "sync readiness check relaxed (REQUIRE_P2P=0)"
fi

coord_http="$(curl -x "" -sS -o "${out_dir}/coordinator_health.json" -w '%{http_code}' "${COORD}/api/network/stats" || true)"
if [[ "${coord_http}" == "200" || "${coord_http}" == "405" ]]; then
  record "coordinator-health" "pass" "coordinator health endpoint reachable"
else
  if [[ "${REQUIRE_COORD_HEALTH}" == "1" ]]; then
    record "coordinator-health" "fail" "coordinator health endpoint unreachable"
  else
    record "coordinator-health" "pass" "coordinator health relaxed (REQUIRE_COORD_HEALTH=0)"
  fi
fi

if [[ "${RUN_DIFFICULTY_HEALTH}" == "1" ]]; then
  if RUN_ID="${RUN_ID}" BASE="${BASE}" bash "${ROOT_DIR}/scripts/tests/difficulty_health.sh" >/dev/null 2>&1; then
    record "difficulty-health-script" "pass" "difficulty_health.sh passed"
  else
    record "difficulty-health-script" "fail" "difficulty_health.sh failed"
  fi
fi

if [[ "${RUN_PRIVATE_STAGE}" == "1" ]]; then
  if RUN_ID="${RUN_ID}" BASE="${BASE}" COORD="${COORD}" ADMIN_TOKEN="${ADMIN_TOKEN}" DO_FREEZE=0 DO_BACKUP=0 REQUIRE_COORD_HEALTH="${REQUIRE_COORD_HEALTH}" \
    bash "${ROOT_DIR}/scripts/ops/private_stage_gate.sh" >/dev/null 2>&1; then
    record "private-stage-gate" "pass" "private_stage_gate passed"
  else
    record "private-stage-gate" "fail" "private_stage_gate failed"
  fi
fi

if [[ -n "${EXPLORER_URL}" ]]; then
  explorer_url_trimmed="${EXPLORER_URL%/}"
  explorer_http="$(curl -x "" -sS -o "${out_dir}/explorer.html" -w '%{http_code}' "${explorer_url_trimmed}/explorer" || true)"
  if [[ "${explorer_http}" == "200" ]] && grep -qi "HackMe Explorer" "${out_dir}/explorer.html"; then
    record "explorer-public-page" "pass" "explorer page reachable on public URL"
  else
    record "explorer-public-page" "fail" "explorer page unavailable or unexpected content"
  fi
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "${results}" | wc -l | tr -d ' ')"
total="$(wc -l <"${results}" | tr -d ' ')"

summary_path="${out_dir}/internet_preflight_summary.json"
jq -nc \
  --arg run_id "${RUN_ID}" \
  --arg base "${BASE}" \
  --arg coord "${COORD}" \
  --argjson total "${total}" \
  --argjson fails "${fails}" \
  --argjson require_p2p "$([[ "${REQUIRE_P2P}" == "1" ]] && echo true || echo false)" \
  --argjson require_coord_health "$([[ "${REQUIRE_COORD_HEALTH}" == "1" ]] && echo true || echo false)" \
  '{
    gate: "internet_preflight_v1",
    run_id: $run_id,
    base: $base,
    coord: $coord,
    total: $total,
    fails: $fails,
    status: (if $fails == 0 then "PASS" else "FAIL" end),
    strictness: {
      require_p2p: $require_p2p,
      require_coord_health: $require_coord_health
    },
    artifacts: {
      status_path: "status.json",
      metrics_path: "metrics.json",
      p2p_path: "p2p_peers.json",
      p2p_sync_path: "p2p_sync.json",
      headers_path: "headers.txt",
      results_path: "results.jsonl"
    }
  }' >"${summary_path}"

cat "${summary_path}"
if [[ "${fails}" != "0" ]]; then
  echo "internet_preflight: FAIL (summary: ${summary_path})" >&2
  exit 1
fi
echo "internet_preflight: PASS (summary: ${summary_path})"
