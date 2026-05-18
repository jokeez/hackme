#!/usr/bin/env bash
set -euo pipefail

# Preflight checks for strict mode readiness.
# - coordinator health
# - p2p peers reachability and quality snapshot
#
# Usage:
#   BASE=http://127.0.0.1:8080 COORD=http://127.0.0.1:8081 scripts/ops/strict_network_preflight.sh
# Optional:
#   AUTO_START_COORD=1 COORD_START_CMD="go run ./cmd/coordinator"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"
RUN_ID="${RUN_ID:-strict_network_preflight_$(date -u +%Y%m%dT%H%M%SZ)}"
AUTO_START_COORD="${AUTO_START_COORD:-0}"
COORD_START_CMD="${COORD_START_CMD:-go run ./cmd/coordinator}"

out_dir="reports/gates/${RUN_ID}"
mkdir -p "${out_dir}"
summary_path="${out_dir}/strict_network_preflight_summary.json"

coord_ok=true
coord_autostarted=false
peer_reachable_count=0

coord_http="$(curl -x "" -sS -o "${out_dir}/coordinator_health.json" -w '%{http_code}' "${COORD}/api/network/stats" || true)"
if [[ "${coord_http}" != "200" && "${coord_http}" != "405" ]]; then
  coord_ok=false
  if [[ "${AUTO_START_COORD}" == "1" ]]; then
    echo "coordinator unavailable, trying auto-start..."
    if nohup bash -lc "${COORD_START_CMD}" > "${out_dir}/coordinator_autostart.log" 2>&1 & then
      sleep 2
      coord_http="$(curl -x "" -sS -o "${out_dir}/coordinator_health.json" -w '%{http_code}' "${COORD}/api/network/stats" || true)"
      if [[ "${coord_http}" == "200" || "${coord_http}" == "405" ]]; then
        coord_ok=true
        coord_autostarted=true
      fi
    fi
  fi
fi

p2p_resp="$(curl -x "" -sS "${BASE}/api/p2p/peers")"
echo "${p2p_resp}" > "${out_dir}/p2p_peers.json"
enabled="$(echo "${p2p_resp}" | jq -r '.enabled // false')"
unstable_count="$(echo "${p2p_resp}" | jq -r '[.peers[]? | select(.unstable == true)] | length')"
bad_count="$(echo "${p2p_resp}" | jq -r '[.peers[]? | select((.quality // "") == "bad")] | length')"
healthy_count="$(echo "${p2p_resp}" | jq -r '[.peers[]? | select(.healthy == true)] | length')"

mapfile -t peer_urls < <(echo "${p2p_resp}" | jq -r '.peers[]?.peer_url // empty')
for peer_url in "${peer_urls[@]:-}"; do
  if [[ -z "${peer_url}" ]]; then
    continue
  fi
  if curl -x "" -sS --max-time 2 "${peer_url}/api/p2p/peers" >/dev/null 2>&1; then
    peer_reachable_count=$((peer_reachable_count + 1))
  fi
done

sync_resp="$(curl -x "" -sS "${BASE}/api/p2p/sync?depth_limit=64")"
echo "${sync_resp}" > "${out_dir}/p2p_sync.json"
sync_needed="$(echo "${sync_resp}" | jq -r '.sync_needed // false')"
sync_blocked="$(echo "${sync_resp}" | jq -r '.sync_blocked // false')"
lag_blocks="$(echo "${sync_resp}" | jq -r '.lag_blocks // 0')"

pass=true
reasons=()
if [[ "${enabled}" != "true" ]]; then pass=false; reasons+=("p2p_disabled"); fi
if [[ "${coord_ok}" != "true" ]]; then pass=false; reasons+=("coordinator_unreachable"); fi
if (( peer_reachable_count < 1 )); then pass=false; reasons+=("no_reachable_peer_endpoint"); fi
if [[ "${sync_blocked}" == "true" ]]; then pass=false; reasons+=("sync_blocked"); fi

jq -n \
  --arg run_id "${RUN_ID}" \
  --arg base "${BASE}" \
  --arg coord "${COORD}" \
  --argjson pass "${pass}" \
  --argjson coordinator_ok "${coord_ok}" \
  --argjson coordinator_autostarted "${coord_autostarted}" \
  --argjson p2p_enabled "$( [[ "${enabled}" == "true" ]] && echo true || echo false )" \
  --argjson unstable_count "${unstable_count}" \
  --argjson bad_count "${bad_count}" \
  --argjson healthy_count "${healthy_count}" \
  --argjson peer_reachable_count "${peer_reachable_count}" \
  --argjson sync_needed "${sync_needed}" \
  --argjson sync_blocked "${sync_blocked}" \
  --argjson lag_blocks "${lag_blocks}" \
  --argjson reasons "$(printf '%s\n' "${reasons[@]:-}" | jq -R . | jq -s 'map(select(length>0))')" \
  '{
    gate: "strict_network_preflight_v1",
    run_id: $run_id,
    base: $base,
    coord: $coord,
    pass: $pass,
    reasons: $reasons,
    coordinator: {
      ok: $coordinator_ok,
      autostarted: $coordinator_autostarted
    },
    p2p: {
      enabled: $p2p_enabled,
      unstable_count: $unstable_count,
      bad_count: $bad_count,
      healthy_count: $healthy_count,
      peer_reachable_count: $peer_reachable_count
    },
    sync: {
      sync_needed: $sync_needed,
      sync_blocked: $sync_blocked,
      lag_blocks: $lag_blocks
    },
    artifacts: {
      coordinator_health_path: "coordinator_health.json",
      p2p_path: "p2p_peers.json",
      p2p_sync_path: "p2p_sync.json"
    }
  }' > "${summary_path}"

cat "${summary_path}"
if [[ "${pass}" != "true" ]]; then
  echo "strict_network_preflight: FAIL (summary: ${summary_path})" >&2
  exit 1
fi
echo "strict_network_preflight: PASS (summary: ${summary_path})"
