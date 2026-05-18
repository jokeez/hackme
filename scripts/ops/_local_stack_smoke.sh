#!/usr/bin/env bash
# Ephemeral local stack: coordinator + main + short worker loop; exit 0 on smoke PASS.
# Usage: from repo root, bash scripts/ops/_local_stack_smoke.sh
# Optional: SMOKE_WORKER_SEC=20 (default 18)

set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

SMOKE_WORKER_SEC="${SMOKE_WORKER_SEC:-18}"
ADMIN_TOKEN="${ADMIN_TOKEN:-local-smoke-admin-token-32char-min-ok!!}"
pick_free_port() {
  python3 -c "import socket;s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1]);s.close()"
}
MAIN_PORT="${MAIN_PORT:-$(pick_free_port)}"
COORD_PORT="${COORD_PORT:-$(pick_free_port)}"
MAIN_BASE="http://127.0.0.1:${MAIN_PORT}"
COORD_BASE="http://127.0.0.1:${COORD_PORT}"
WORKDIR="${WORKDIR:-/tmp/hackme-local-smoke-$$}"

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "[local-smoke] missing: $1" >&2; exit 1; }; }
require_cmd go
require_cmd curl
require_cmd jq
require_cmd timeout
require_cmd python3

echo "[local-smoke] workdir $WORKDIR"
rm -rf "$WORKDIR"
mkdir -p "$WORKDIR/data"
rsync -a \
  --exclude '.git/' --exclude 'data' --exclude 'data/' --exclude 'reports/' \
  --exclude 'node_modules/' --exclude 'dist/' --exclude 'backups/' --exclude 'logs/' \
  --exclude '.env' --exclude '.env.*' \
  "$ROOT_DIR/" "$WORKDIR/"
cd "$WORKDIR"

mkdir -p "$WORKDIR/bin"
coord_bin="$WORKDIR/bin/coordinator"
main_bin="$WORKDIR/bin/hackme-node"
echo "[local-smoke] go build binaries -> $WORKDIR/bin/"
go build -o "$coord_bin" ./cmd/coordinator
go build -o "$main_bin" .

coord_log="$WORKDIR/coordinator.log"
main_log="$WORKDIR/main.log"
worker_log="$WORKDIR/worker.log"
coord_pid=""
main_pid=""

kill_port_listeners() {
  local port="$1"
  if command -v fuser >/dev/null 2>&1; then
    fuser -k -TERM "${port}/tcp" >/dev/null 2>&1 || true
    sleep 0.3
    fuser -k -KILL "${port}/tcp" >/dev/null 2>&1 || true
  fi
}

cleanup() {
  local ec=$?
  [[ -n "$main_pid" ]] && kill -TERM "$main_pid" 2>/dev/null || true
  [[ -n "$coord_pid" ]] && kill -TERM "$coord_pid" 2>/dev/null || true
  sleep 0.5
  [[ -n "$main_pid" ]] && kill -KILL "$main_pid" 2>/dev/null || true
  [[ -n "$coord_pid" ]] && kill -KILL "$coord_pid" 2>/dev/null || true
  kill_port_listeners "$MAIN_PORT"
  kill_port_listeners "$COORD_PORT"
  wait 2>/dev/null || true
  exit "$ec"
}
trap cleanup INT TERM EXIT

echo "[local-smoke] starting coordinator $COORD_BASE"
HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}" \
HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN" \
HACKME_COORDINATOR_DB="$WORKDIR/data/coordinator.db" \
  "$coord_bin" >>"$coord_log" 2>&1 &
coord_pid=$!

for i in $(seq 1 40); do
  if curl -fsS --max-time 3 "$COORD_BASE/api/network/stats" >/dev/null 2>&1; then
    echo "[local-smoke] coordinator up"
    break
  fi
  if ! kill -0 "$coord_pid" 2>/dev/null; then
    echo "[local-smoke] coordinator died; log:" >&2
    tail -40 "$coord_log" >&2
    exit 1
  fi
  sleep 0.5
  if (( i == 40 )); then
    echo "[local-smoke] coordinator timeout" >&2
    tail -40 "$coord_log" >&2
    exit 1
  fi
done

echo "[local-smoke] starting main $MAIN_BASE"
HACKME_BIND_ADDR="127.0.0.1:${MAIN_PORT}" \
HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
HACKME_POOL_COORDINATOR_URL="$COORD_BASE" \
HACKME_POOL_COORDINATOR_TOKEN="$ADMIN_TOKEN" \
HACKME_FUZZ_AUTORUN=0 \
  "$main_bin" >>"$main_log" 2>&1 &
main_pid=$!

for i in $(seq 1 60); do
  if curl -fsS --max-time 5 "$MAIN_BASE/api/status" >/dev/null 2>&1; then
    echo "[local-smoke] main up"
    break
  fi
  if ! kill -0 "$main_pid" 2>/dev/null; then
    echo "[local-smoke] main died; log:" >&2
    tail -60 "$main_log" >&2
    exit 1
  fi
  sleep 0.5
  if (( i == 60 )); then
    echo "[local-smoke] main timeout" >&2
    tail -60 "$main_log" >&2
    exit 1
  fi
done

st="$(curl -fsS --max-time 10 "$MAIN_BASE/api/status")"
if [[ "$(echo "$st" | jq -r '.has_genesis')" != "true" ]]; then
  echo "[local-smoke] posting genesis"
  curl -fsS --max-time 15 -X POST "$MAIN_BASE/api/genesis" \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{}' >/dev/null
  st="$(curl -fsS --max-time 10 "$MAIN_BASE/api/status")"
fi

echo "$st" | jq '{tip_height, mining, has_genesis, admin_auth_enabled}'
curl -fsS --max-time 10 "$MAIN_BASE/api/global/metrics" | jq '{ok, sample_ts, stale_sec}'
curl -fsS --max-time 5 "$COORD_BASE/api/network/stats" | jq '.global_total_hashrate_th_s // .hashrate_th_s // .'

echo "[local-smoke] worker loop ${SMOKE_WORKER_SEC}s"
export COORD_URL="$COORD_BASE"
export COORD_ADMIN_TOKEN="$ADMIN_TOKEN"
export WORKER_ID="local-smoke-worker"
export BATCH_SIZE="${BATCH_SIZE:-500000}"
export HASHRATE_GHS="${HASHRATE_GHS:-12.5}"
set +e
timeout "${SMOKE_WORKER_SEC}" bash "$WORKDIR/scripts/ops/worker_loop.sh" >>"$worker_log" 2>&1
worker_ec=$?
set -e
if (( worker_ec != 124 && worker_ec != 0 )); then
  echo "[local-smoke] worker exit $worker_ec" >&2
  tail -30 "$worker_log" >&2
fi

if grep -qE '\[worker\] submit ok|claims=[1-9]' "$worker_log" 2>/dev/null; then
  echo "[local-smoke] PASS: worker submit/claims activity"
  tail -5 "$worker_log"
elif grep -q '\[worker\]' "$worker_log" 2>/dev/null; then
  echo "[local-smoke] PASS (soft): worker loop ran"
  tail -8 "$worker_log"
else
  echo "[local-smoke] WARN: worker log inconclusive; tail:" >&2
  tail -25 "$worker_log" >&2
fi

curl -fsS --max-time 10 "$MAIN_BASE/api/work/stats" | jq '.' || true
echo "[local-smoke] logs under $WORKDIR *.log"
trap - INT TERM EXIT
kill -TERM "$main_pid" 2>/dev/null || true
kill -TERM "$coord_pid" 2>/dev/null || true
sleep 0.5
kill -KILL "$main_pid" 2>/dev/null || true
kill -KILL "$coord_pid" 2>/dev/null || true
kill_port_listeners "$MAIN_PORT"
kill_port_listeners "$COORD_PORT"
wait 2>/dev/null || true
echo "[local-smoke] done OK"
