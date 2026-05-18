#!/usr/bin/env bash
set -euo pipefail

# Unified final gate for fuzz + policy + p2p + retention.
#
# Usage:
#   ADMIN_TOKEN=... BASE=http://127.0.0.1:8080 scripts/ops/fuzz_super_gate.sh
# Optional:
#   RUN_ID=fuzz_super_gate_... P2P_MAX_UNSTABLE=1 P2P_MAX_BAD=1 P2P_REQUIRE_HEALTHY=0
#   STRICT_MODE=1 HARD_FAIL_PRIVATE_STAGE=1 COORD=http://127.0.0.1:8081

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
RUN_ID="${RUN_ID:-fuzz_super_gate_$(date -u +%Y%m%dT%H%M%SZ)}"
P2P_MAX_UNSTABLE="${P2P_MAX_UNSTABLE:-1}"
P2P_MAX_BAD="${P2P_MAX_BAD:-1}"
P2P_REQUIRE_HEALTHY="${P2P_REQUIRE_HEALTHY:-0}"
STRICT_MODE="${STRICT_MODE:-0}"
HARD_FAIL_PRIVATE_STAGE="${HARD_FAIL_PRIVATE_STAGE:-0}"
COORD="${COORD:-http://127.0.0.1:8081}"

if [[ "${STRICT_MODE}" == "1" ]]; then
  P2P_MAX_UNSTABLE=0
  P2P_MAX_BAD=0
  P2P_REQUIRE_HEALTHY=1
fi

if [[ -z "${ADMIN_TOKEN}" ]]; then
  echo "fuzz_super_gate: ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 2
fi

out_dir="reports/gates/${RUN_ID}"
mkdir -p "${out_dir}"
summary_path="${out_dir}/fuzz_super_gate_summary.json"

release_ok=true
housekeeping_ok=true
p2p_ok=true
policy_ok=true
coordinator_ok=true
sync_ok=true

echo "[1/4] fuzz release gate"
if ! ADMIN_TOKEN="${ADMIN_TOKEN}" BASE="${BASE}" scripts/ops/fuzz_release_gate.sh; then
  release_ok=false
fi

echo "[2/4] fuzz housekeeping sweep"
if ! ADMIN_TOKEN="${ADMIN_TOKEN}" BASE="${BASE}" scripts/ops/fuzz_housekeeping_sweep.sh; then
  housekeeping_ok=false
fi

echo "[3/4] p2p quality contract"
p2p_resp="$(curl -x "" -sS "${BASE}/api/p2p/peers")"
echo "${p2p_resp}" > "${out_dir}/p2p_peers.json"
enabled="$(echo "${p2p_resp}" | jq -r '.enabled // false')"
unstable_count="$(echo "${p2p_resp}" | jq -r '[.peers[]? | select(.unstable == true)] | length')"
bad_count="$(echo "${p2p_resp}" | jq -r '[.peers[]? | select((.quality // "") == "bad")] | length')"
healthy_count="$(echo "${p2p_resp}" | jq -r '[.peers[]? | select(.healthy == true)] | length')"
if [[ "${enabled}" == "true" ]]; then
  if (( unstable_count > P2P_MAX_UNSTABLE )); then
    p2p_ok=false
  fi
  if (( bad_count > P2P_MAX_BAD )); then
    p2p_ok=false
  fi
  if [[ "${P2P_REQUIRE_HEALTHY}" == "1" ]] && (( healthy_count < 1 )); then
    p2p_ok=false
  fi
fi

echo "[4/4] policy contract snapshot"
status_resp="$(curl -x "" -sS "${BASE}/api/status")"
echo "${status_resp}" > "${out_dir}/status.json"
if ! echo "${status_resp}" | jq -e '.sandbox_policy.locked == true' >/dev/null; then
  policy_ok=false
fi
if ! echo "${status_resp}" | jq -e '.sandbox_policy.block_quarantined == true' >/dev/null; then
  policy_ok=false
fi
if ! echo "${status_resp}" | jq -e '(.economics.policy_hash | type) == "string" and (.economics.policy_hash | length) > 0' >/dev/null; then
  policy_ok=false
fi

sync_resp="$(curl -x "" -sS "${BASE}/api/p2p/sync?depth_limit=64")"
echo "${sync_resp}" > "${out_dir}/p2p_sync.json"
sync_enabled="$(echo "${sync_resp}" | jq -r '.enabled // false')"
sync_blocked="$(echo "${sync_resp}" | jq -r '.sync_blocked // false')"
sync_needed="$(echo "${sync_resp}" | jq -r '.sync_needed // false')"
lag_blocks="$(echo "${sync_resp}" | jq -r '.lag_blocks // 0')"
if [[ "${sync_enabled}" == "true" ]]; then
  if [[ "${sync_blocked}" == "true" ]]; then
    sync_ok=false
  fi
  if [[ "${STRICT_MODE}" == "1" && "${sync_needed}" == "true" ]]; then
    sync_ok=false
  fi
fi

echo "[extra] coordinator/private-stage contract"
coord_http="$(curl -x "" -sS -o "${out_dir}/coordinator_health.json" -w '%{http_code}' "${COORD}/api/network/stats" || true)"
if [[ "${coord_http}" == "200" || "${coord_http}" == "405" ]]; then
  coordinator_ok=true
else
  coordinator_ok=false
fi

pass=true
reasons=()
if [[ "${release_ok}" != "true" ]]; then reasons+=("release_gate_failed"); pass=false; fi
if [[ "${housekeeping_ok}" != "true" ]]; then reasons+=("housekeeping_sweep_failed"); pass=false; fi
if [[ "${p2p_ok}" != "true" ]]; then reasons+=("p2p_quality_contract_failed"); pass=false; fi
if [[ "${policy_ok}" != "true" ]]; then reasons+=("policy_contract_failed"); pass=false; fi
if [[ "${sync_ok}" != "true" ]]; then reasons+=("p2p_sync_contract_failed"); pass=false; fi
if [[ "${HARD_FAIL_PRIVATE_STAGE}" == "1" && "${coordinator_ok}" != "true" ]]; then reasons+=("coordinator_health_unreachable"); pass=false; fi

jq -n \
  --arg run_id "${RUN_ID}" \
  --arg base "${BASE}" \
  --argjson pass "${pass}" \
  --argjson release_ok "${release_ok}" \
  --argjson housekeeping_ok "${housekeeping_ok}" \
  --argjson p2p_ok "${p2p_ok}" \
  --argjson policy_ok "${policy_ok}" \
  --argjson coordinator_ok "${coordinator_ok}" \
  --argjson sync_ok "${sync_ok}" \
  --argjson strict_mode "${STRICT_MODE}" \
  --argjson hard_fail_private_stage "${HARD_FAIL_PRIVATE_STAGE}" \
  --argjson unstable_count "${unstable_count}" \
  --argjson bad_count "${bad_count}" \
  --argjson healthy_count "${healthy_count}" \
  --argjson p2p_max_unstable "${P2P_MAX_UNSTABLE}" \
  --argjson p2p_max_bad "${P2P_MAX_BAD}" \
  --argjson p2p_require_healthy "${P2P_REQUIRE_HEALTHY}" \
  --argjson sync_needed "${sync_needed}" \
  --argjson sync_blocked "${sync_blocked}" \
  --argjson lag_blocks "${lag_blocks}" \
  --argjson reasons "$(printf '%s\n' "${reasons[@]:-}" | jq -R . | jq -s 'map(select(length>0))')" \
  '{
    gate: "fuzz_super_gate_v1",
    run_id: $run_id,
    base: $base,
    pass: $pass,
    reasons: $reasons,
    checks: {
      release_gate: $release_ok,
      housekeeping_sweep: $housekeeping_ok,
      p2p_quality: $p2p_ok,
      policy_contract: $policy_ok,
      p2p_sync_contract: $sync_ok,
      coordinator_health: $coordinator_ok
    },
    mode: {
      strict_mode: $strict_mode,
      hard_fail_private_stage: $hard_fail_private_stage
    },
    p2p: {
      unstable_count: $unstable_count,
      bad_count: $bad_count,
      healthy_count: $healthy_count,
      sync_needed: $sync_needed,
      sync_blocked: $sync_blocked,
      lag_blocks: $lag_blocks,
      thresholds: {
        max_unstable: $p2p_max_unstable,
        max_bad: $p2p_max_bad,
        require_healthy: $p2p_require_healthy
      }
    },
    artifacts: {
      status_path: "status.json",
      p2p_path: "p2p_peers.json",
      p2p_sync_path: "p2p_sync.json",
      coordinator_health_path: "coordinator_health.json"
    }
  }' > "${summary_path}"

cat "${summary_path}"
if [[ "${pass}" != "true" ]]; then
  echo "fuzz_super_gate: FAIL (summary: ${summary_path})" >&2
  exit 1
fi
echo "fuzz_super_gate: PASS (summary: ${summary_path})"
