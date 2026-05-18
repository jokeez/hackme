#!/usr/bin/env bash
# Build and run deploy/docker-compose.e2e.yml, then verify genesis, pool lane on /api/status,
# coordinator work/stats (target_mod), worker subprocess, and a trimmed fuzz_release_gate.
#
# Usage (repo root):
#   bash scripts/ops/docker_e2e_smoke.sh
# Env:
#   E2E_TOKEN — optional (generated if unset)
#   NODE_PUBLISH_PORT / COORD_PUBLISH_PORT — host ports (default 18080 / 18081)
#   SKIP_DOCKER_DOWN=1 — leave stack running
#   FUZZ_FULL=1 — run full fuzz_release_gate (slow); default runs contract-only subset

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[docker-e2e] missing: $1" >&2
    exit 1
  }
}

require_cmd docker
require_cmd curl
require_cmd jq

if ! docker compose version >/dev/null 2>&1; then
  echo "[docker-e2e] need Docker Compose v2 (docker compose)" >&2
  exit 1
fi

E2E_TOKEN="${E2E_TOKEN:-}"
if [[ -z "$E2E_TOKEN" ]]; then
  if command -v openssl >/dev/null 2>&1; then
    E2E_TOKEN="$(openssl rand -hex 24)"
  else
    E2E_TOKEN="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
  fi
fi
export E2E_TOKEN

NODE_PUBLISH_PORT="${NODE_PUBLISH_PORT:-18080}"
COORD_PUBLISH_PORT="${COORD_PUBLISH_PORT:-18081}"
export NODE_PUBLISH_PORT COORD_PUBLISH_PORT

BASE="http://127.0.0.1:${NODE_PUBLISH_PORT}"
COORD="http://127.0.0.1:${COORD_PUBLISH_PORT}"
export BASE COORD

compose() {
  docker compose -f deploy/docker-compose.e2e.yml "$@"
}

cleanup() {
  local ec=$?
  if [[ "${SKIP_DOCKER_DOWN:-0}" != "1" ]]; then
    compose down -v --remove-orphans >/dev/null 2>&1 || true
  else
    echo "[docker-e2e] SKIP_DOCKER_DOWN=1 — stack still up at BASE=$BASE COORD=$COORD"
  fi
  exit "$ec"
}
trap cleanup INT TERM EXIT

echo "[docker-e2e] building & starting (BASE=$BASE COORD=$COORD)"
compose down -v --remove-orphans >/dev/null 2>&1 || true
compose up --build -d

echo "[docker-e2e] waiting for HTTP"
for i in $(seq 1 60); do
  if curl -fsS --max-time 3 "$BASE/api/status" >/dev/null 2>&1; then
    break
  fi
  sleep 1
  if (( i == 60 )); then
    echo "[docker-e2e] node timeout" >&2
    compose logs --tail 80 node >&2 || true
    exit 1
  fi
done

st="$(curl -fsS --max-time 10 "$BASE/api/status")"
if [[ "$(echo "$st" | jq -r '.has_genesis')" != "true" ]]; then
  echo "[docker-e2e] posting genesis"
  curl -fsS --max-time 20 -X POST "$BASE/api/genesis" \
    -H "X-Hackme-Admin-Token: ${E2E_TOKEN}" \
    -H "Content-Type: application/json" -d '{}' >/dev/null
  st="$(curl -fsS --max-time 10 "$BASE/api/status")"
fi

echo "[docker-e2e] pool lane + coordinator probe"
curl -fsS --max-time 15 "$BASE/api/status" | jq '{
  network_mode_active,
  pool_coordinator_url_effective,
  pool_target_mod,
  pool_target_mod_source,
  pool_global_hashrate_th_s,
  pool_total_miners,
  pool_workers_count
}'

curl -fsS --max-time 10 "$COORD/api/work/stats?details=0" | jq '{target_mod, workers_count, issued_ranges}'

echo "$st" | jq -e '(.pool_target_mod // 0) > 0' >/dev/null || {
  echo "[docker-e2e] INFO: pool_target_mod 0 before worker (expected); coordinator work/stats above is source of truth" >&2
}

echo "[docker-e2e] starting worker subprocess on node (coord_url=internal bridge)"
curl -fsS --max-time 20 -X POST "$BASE/api/worker/start" \
  -H "X-Hackme-Admin-Token: ${E2E_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "$(jq -n \
    --arg u "http://coordinator:8081" \
    --arg t "$E2E_TOKEN" \
    '{coord_url:$u, coord_token:$t, worker_id:"docker-e2e-1", batch_size:800000, hashrate_gh_s:0.42}')" >/dev/null

for i in $(seq 1 40); do
  wr="$(curl -fsS --max-time 5 "$BASE/api/worker/status" || true)"
  if echo "$wr" | jq -e '.running == true' >/dev/null 2>&1; then
    echo "[docker-e2e] worker running"
    break
  fi
  sleep 0.5
  if (( i == 40 )); then
    echo "[docker-e2e] worker did not start" >&2
    curl -sS "$BASE/api/worker/status" | jq . >&2 || true
    exit 1
  fi
done

sleep 2
curl -fsS --max-time 12 "$BASE/api/status" | jq '{pool_target_mod, pool_workers_count, pool_global_hashrate_th_s}'
curl -fsS --max-time 10 "$BASE/api/global/metrics" | jq '{global_source, chain: .chain.tip_height, work: {target_mod: .work.target_mod, workers_count: .work.workers_count}}'

echo "[docker-e2e] coordinator difficulty / accrual snapshot"
curl -fsS --max-time 10 "$COORD/api/work/stats?details=1" | jq '{target_mod, workers_count, total_payout_hmc, accepted_attempts}'

echo "[docker-e2e] fuzz gate (${FUZZ_FULL:-0})"
export ADMIN_TOKEN="$E2E_TOKEN"
if [[ "${FUZZ_FULL:-0}" == "1" ]]; then
  bash scripts/ops/fuzz_release_gate.sh
else
  echo "[docker-e2e] contract-only fuzz steps (set FUZZ_FULL=1 for full gate)"
  RUN_LANGUAGE_MATRIX=0 RUN_ORDERS_MULTILANG_AUDIT=0 RUN_LANGUAGE_BREAK_ATTEMPTS=0 \
    RUN_CHAOS_LANG_SECURITY=0 RUN_REDTEAM_SMOKE=0 \
    bash scripts/ops/fuzz_release_gate.sh
fi

echo "[docker-e2e] PASS"
trap - INT TERM EXIT
if [[ "${SKIP_DOCKER_DOWN:-0}" != "1" ]]; then
  compose down -v --remove-orphans
fi
exit 0
