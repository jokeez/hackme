#!/usr/bin/env bash
# Simulate N independent pool workers (different WORKER_ID) on one host — like several PCs
# behind the same coordinator. Uses ephemeral coordinator + command node (same pattern as
# _local_stack_smoke.sh), then runs worker_loop in parallel for DEMO_SEC each.
#
# Usage (repo root):
#   bash scripts/ops/simulate_pool_swarm_local.sh
# Env:
#   DEMO_SEC=45          — seconds each worker runs (default 40)
#   WORKER_COUNT=4       — parallel workers (default 3)
#   ADMIN_TOKEN          — optional; default generated-like smoke script

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

DEMO_SEC="${DEMO_SEC:-40}"
WORKER_COUNT="${WORKER_COUNT:-3}"
ADMIN_TOKEN="${ADMIN_TOKEN:-local-swarm-admin-token-32char-min-ok!!}"

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "[pool-swarm] missing: $1" >&2; exit 1; }; }
require_cmd go
require_cmd curl
require_cmd jq
require_cmd timeout
require_cmd python3

pick_free_port() {
  python3 -c "import socket;s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1]);s.close()"
}
MAIN_PORT="$(pick_free_port)"
COORD_PORT="$(pick_free_port)"
MAIN_BASE="http://127.0.0.1:${MAIN_PORT}"
COORD_BASE="http://127.0.0.1:${COORD_PORT}"
WORKDIR="${WORKDIR:-/tmp/hackme-pool-swarm-$$}"

kill_port_listeners() {
  local port="$1"
  if command -v fuser >/dev/null 2>&1; then
    fuser -k -TERM "${port}/tcp" >/dev/null 2>&1 || true
    sleep 0.25
    fuser -k -KILL "${port}/tcp" >/dev/null 2>&1 || true
  fi
}

coord_pid=""
main_pid=""
pids=()

cleanup() {
  local ec=$?
  for p in "${pids[@]:-}"; do
    kill -TERM "$p" 2>/dev/null || true
  done
  wait 2>/dev/null || true
  [[ -n "$main_pid" ]] && kill -TERM "$main_pid" 2>/dev/null || true
  [[ -n "$coord_pid" ]] && kill -TERM "$coord_pid" 2>/dev/null || true
  sleep 0.4
  [[ -n "$main_pid" ]] && kill -KILL "$main_pid" 2>/dev/null || true
  [[ -n "$coord_pid" ]] && kill -KILL "$coord_pid" 2>/dev/null || true
  kill_port_listeners "$MAIN_PORT"
  kill_port_listeners "$COORD_PORT"
  exit "$ec"
}
trap cleanup INT TERM EXIT

rm -rf "$WORKDIR"
mkdir -p "$WORKDIR/data"
rsync -a \
  --exclude '.git/' --exclude 'data' --exclude 'data/' --exclude 'reports/' \
  --exclude 'node_modules/' --exclude 'dist/' --exclude 'backups/' --exclude 'logs/' \
  --exclude '.env' --exclude '.env.*' \
  "$ROOT_DIR/" "$WORKDIR/"
cd "$WORKDIR"
mkdir -p "$WORKDIR/bin"
go build -o "$WORKDIR/bin/coordinator" ./cmd/coordinator
go build -o "$WORKDIR/bin/hackme-node" .

echo "[pool-swarm] MAIN=$MAIN_BASE COORD=$COORD_BASE workers=$WORKER_COUNT demo=${DEMO_SEC}s"

HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}" \
HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN" \
HACKME_COORDINATOR_DB="$WORKDIR/data/coordinator.db" \
  "$WORKDIR/bin/coordinator" >>"$WORKDIR/coordinator.log" 2>&1 &
coord_pid=$!

for i in $(seq 1 50); do
  if curl -fsS --max-time 2 "$COORD_BASE/api/network/stats" >/dev/null 2>&1; then break; fi
  if ! kill -0 "$coord_pid" 2>/dev/null; then echo "[pool-swarm] coordinator died" >&2; tail -20 "$WORKDIR/coordinator.log" >&2; exit 1; fi
  sleep 0.3
  if (( i == 50 )); then echo "[pool-swarm] coordinator timeout" >&2; exit 1; fi
done

HACKME_BIND_ADDR="127.0.0.1:${MAIN_PORT}" \
HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
HACKME_POOL_COORDINATOR_URL="$COORD_BASE" \
HACKME_POOL_COORDINATOR_TOKEN="$ADMIN_TOKEN" \
HACKME_FUZZ_AUTORUN=0 \
  "$WORKDIR/bin/hackme-node" >>"$WORKDIR/main.log" 2>&1 &
main_pid=$!

for i in $(seq 1 80); do
  if curl -fsS --max-time 3 "$MAIN_BASE/api/status" >/dev/null 2>&1; then break; fi
  if ! kill -0 "$main_pid" 2>/dev/null; then echo "[pool-swarm] node died" >&2; tail -30 "$WORKDIR/main.log" >&2; exit 1; fi
  sleep 0.3
  if (( i == 80 )); then echo "[pool-swarm] node timeout" >&2; exit 1; fi
done

if [[ "$(curl -fsS --max-time 8 "$MAIN_BASE/api/status" | jq -r '.has_genesis')" != "true" ]]; then
  curl -fsS --max-time 15 -X POST "$MAIN_BASE/api/genesis" \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{}' >/dev/null
fi

export COORD_URL="$COORD_BASE"
export COORD_ADMIN_TOKEN="$ADMIN_TOKEN"
export BATCH_SIZE="${BATCH_SIZE:-400000}"
export HASHRATE_GHS="${HASHRATE_GHS:-15}"

echo "[pool-swarm] starting $WORKER_COUNT parallel worker_loop (${DEMO_SEC}s each)…"
for w in $(seq 1 "$WORKER_COUNT"); do
  wid="sim-swarm-$(printf '%02d' "$w")"
  logf="$WORKDIR/worker-${wid}.log"
  (
    export WORKER_ID="$wid"
    export WORKER_NAME="Swarm-$wid"
    exec timeout "${DEMO_SEC}" bash "$WORKDIR/scripts/ops/worker_loop.sh"
  ) >>"$logf" 2>&1 &
  pids+=("$!")
done

# Wait all workers
for p in "${pids[@]}"; do
  wait "$p" || true
done
pids=()

echo "[pool-swarm] coordinator work/stats (per-worker payout snapshot):"
curl -fsS --max-time 15 "${COORD_BASE}/api/work/stats?details=1" \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" | jq '{
  workers_count,
  issued_ranges,
  submitted_items,
  accepted_attempts,
  total_payout_hmc,
  target_mod,
  workers: (.workers // {})
}'

echo "[pool-swarm] per-worker log tail (last line each):"
for w in $(seq 1 "$WORKER_COUNT"); do
  wid="sim-swarm-$(printf '%02d' "$w")"
  logf="$WORKDIR/worker-${wid}.log"
  echo "--- ${wid} ---"
  tail -n 2 "$logf" 2>/dev/null || echo "(no log)"
done

echo "[pool-swarm] done OK (workdir $WORKDIR logs retained until exit)"
exit 0
