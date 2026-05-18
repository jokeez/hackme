#!/usr/bin/env bash
set -euo pipefail

# Recovery helper for strict release profile.
# Diagnoses strict blockers, optionally attempts p2p sync, then retries strict super-gate.
#
# Usage:
#   ADMIN_TOKEN=... BASE=http://127.0.0.1:8080 COORD=http://127.0.0.1:8081 scripts/ops/fuzz_strict_recover.sh
# Optional:
#   RETRIES=3 RETRY_SLEEP_SEC=5 ATTEMPT_SYNC=1 RUN_ID=...

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
RUN_ID="${RUN_ID:-fuzz_strict_recover_$(date -u +%Y%m%dT%H%M%SZ)}"
RETRIES="${RETRIES:-3}"
RETRY_SLEEP_SEC="${RETRY_SLEEP_SEC:-5}"
ATTEMPT_SYNC="${ATTEMPT_SYNC:-1}"
RUN_PREFLIGHT="${RUN_PREFLIGHT:-1}"
AUTO_START_COORD="${AUTO_START_COORD:-0}"

if [[ -z "${ADMIN_TOKEN}" ]]; then
  echo "fuzz_strict_recover: ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 2
fi

out_dir="reports/gates/${RUN_ID}"
mkdir -p "${out_dir}"
diag_path="${out_dir}/strict_recover_diag.json"

if [[ "${RUN_PREFLIGHT}" == "1" ]]; then
  echo "running strict preflight..."
  if ! RUN_ID="${RUN_ID}_preflight" BASE="${BASE}" COORD="${COORD}" AUTO_START_COORD="${AUTO_START_COORD}" scripts/ops/strict_network_preflight.sh; then
    echo "strict preflight reports unresolved blockers; continuing with diagnostics/retry flow..."
  fi
fi

status_resp="$(curl -x "" -sS "${BASE}/api/status")"
p2p_resp="$(curl -x "" -sS "${BASE}/api/p2p/peers")"
sync_resp="$(curl -x "" -sS "${BASE}/api/p2p/sync?depth_limit=64")"
coord_ok=true
coord_http="$(curl -x "" -sS -o "${out_dir}/coordinator_health.json" -w '%{http_code}' "${COORD}/api/network/stats" || true)"
if [[ "${coord_http}" != "200" && "${coord_http}" != "405" ]]; then
  coord_ok=false
fi

echo "${status_resp}" > "${out_dir}/status.json"
echo "${p2p_resp}" > "${out_dir}/p2p_peers.json"
echo "${sync_resp}" > "${out_dir}/p2p_sync.json"

unstable_count="$(echo "${p2p_resp}" | jq -r '[.peers[]? | select(.unstable == true)] | length')"
bad_count="$(echo "${p2p_resp}" | jq -r '[.peers[]? | select((.quality // "") == "bad")] | length')"
healthy_count="$(echo "${p2p_resp}" | jq -r '[.peers[]? | select(.healthy == true)] | length')"
sync_needed="$(echo "${sync_resp}" | jq -r '.sync_needed // false')"
sync_blocked="$(echo "${sync_resp}" | jq -r '.sync_blocked // false')"
sync_action="$(echo "${sync_resp}" | jq -r '.sync_action // ""')"
lag_blocks="$(echo "${sync_resp}" | jq -r '.lag_blocks // 0')"

reasons=()
if (( unstable_count > 0 )); then reasons+=("p2p_unstable_peers_present"); fi
if (( bad_count > 0 )); then reasons+=("p2p_bad_peers_present"); fi
if (( healthy_count < 1 )); then reasons+=("no_healthy_peers"); fi
if [[ "${sync_blocked}" == "true" ]]; then reasons+=("sync_blocked"); fi
if [[ "${sync_needed}" == "true" ]]; then reasons+=("sync_needed"); fi
if [[ "${coord_ok}" != "true" ]]; then reasons+=("coordinator_unreachable"); fi

if [[ "${ATTEMPT_SYNC}" == "1" && "${sync_needed}" == "true" ]]; then
  echo "attempting p2p sync/run..."
  sync_run_resp="$(curl -x "" -sS -X POST \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
    "${BASE}/api/p2p/sync/run?depth_limit=64&max_apply=50" || true)"
  echo "${sync_run_resp}" > "${out_dir}/sync_run.json"
fi

jq -n \
  --arg run_id "${RUN_ID}" \
  --arg base "${BASE}" \
  --arg coord "${COORD}" \
  --argjson unstable_count "${unstable_count}" \
  --argjson bad_count "${bad_count}" \
  --argjson healthy_count "${healthy_count}" \
  --argjson lag_blocks "${lag_blocks}" \
  --argjson sync_needed "${sync_needed}" \
  --argjson sync_blocked "${sync_blocked}" \
  --arg sync_action "${sync_action}" \
  --argjson coordinator_ok "${coord_ok}" \
  --argjson reasons "$(printf '%s\n' "${reasons[@]:-}" | jq -R . | jq -s 'map(select(length>0))')" \
  '{
    gate: "fuzz_strict_recover_v1",
    run_id: $run_id,
    base: $base,
    coord: $coord,
    issues: $reasons,
    p2p: {
      unstable_count: $unstable_count,
      bad_count: $bad_count,
      healthy_count: $healthy_count
    },
    sync: {
      lag_blocks: $lag_blocks,
      sync_needed: $sync_needed,
      sync_blocked: $sync_blocked,
      sync_action: $sync_action
    },
    coordinator_ok: $coordinator_ok
  }' > "${diag_path}"

echo "strict diagnostics:"
cat "${diag_path}"

if (( ${#reasons[@]} > 0 )); then
  echo
  echo "remediation hints:"
  if (( healthy_count < 1 )); then
    echo "- Ensure at least one reachable healthy peer in HACKME_P2P_PEERS."
    echo "- Check route/firewall/NAT to peer hosts (current healthy_count=${healthy_count})."
  fi
  if [[ "${sync_needed}" == "true" || "${sync_blocked}" == "true" ]]; then
    echo "- Inspect /api/p2p/sync and run admin sync loop until lag_blocks=0."
  fi
  if [[ "${coord_ok}" != "true" ]]; then
    echo "- Start coordinator and verify ${COORD}/api/network/stats is reachable."
  fi
fi

attempt=1
while (( attempt <= RETRIES )); do
  rid="${RUN_ID}_gate_try${attempt}"
  echo
  echo "strict super gate attempt ${attempt}/${RETRIES}..."
  if ADMIN_TOKEN="${ADMIN_TOKEN}" BASE="${BASE}" COORD="${COORD}" RUN_ID="${rid}" STRICT_MODE=1 scripts/ops/fuzz_super_gate.sh; then
    echo "fuzz_strict_recover: PASS (strict profile reached)"
    exit 0
  fi
  if (( attempt < RETRIES )); then
    echo "strict gate failed, retrying in ${RETRY_SLEEP_SEC}s..."
    sleep "${RETRY_SLEEP_SEC}"
  fi
  attempt=$((attempt + 1))
done

echo "fuzz_strict_recover: FAIL (strict profile not reached)" >&2
exit 1
