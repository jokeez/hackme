#!/usr/bin/env bash
set -euo pipefail

# One-command local stack:
# - main node
# - coordinator
# - optional local worker loop
# with health checks and clear "what failed" status.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[stack] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd go

ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[stack] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 1
fi
if [[ "$ADMIN_TOKEN" == *"..."* || "$ADMIN_TOKEN" == *"PUT_FULL_TOKEN_HERE"* || "$ADMIN_TOKEN" == *"CHANGE_ME"* ]]; then
  echo "[stack] ADMIN_TOKEN looks like placeholder; set real token value" >&2
  exit 1
fi

MAIN_ADDR="${MAIN_ADDR:-0.0.0.0:8080}"
MAIN_BASE="${MAIN_BASE:-http://127.0.0.1:8080}"
COORD_ADDR="${COORD_ADDR:-0.0.0.0:8081}"
COORD_BASE="${COORD_BASE:-http://127.0.0.1:8081}"
START_LOCAL_WORKER="${START_LOCAL_WORKER:-1}"
WORKER_ID="${WORKER_ID:-stack-local-worker}"
WORKER_BATCH_SIZE="${WORKER_BATCH_SIZE:-2000000}"
WORKER_HASHRATE_GHS="${WORKER_HASHRATE_GHS:-42.5}"

LOG_DIR="${LOG_DIR:-$ROOT_DIR/logs/stack}"
mkdir -p "$LOG_DIR"

main_pid=""
coord_pid=""
worker_pid=""

cleanup() {
  local ec=$?
  [[ -n "$worker_pid" ]] && kill "$worker_pid" >/dev/null 2>&1 || true
  [[ -n "$main_pid" ]] && kill "$main_pid" >/dev/null 2>&1 || true
  [[ -n "$coord_pid" ]] && kill "$coord_pid" >/dev/null 2>&1 || true
  wait >/dev/null 2>&1 || true
  exit "$ec"
}
trap cleanup INT TERM EXIT

wait_http_ok() {
  local name="$1"
  local url="$2"
  local max="${3:-40}"
  local i=1
  while (( i <= max )); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      echo "[stack] ${name} healthy: ${url}"
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done
  echo "[stack] ${name} failed health check: ${url}" >&2
  return 1
}

echo "[stack] starting coordinator -> ${COORD_ADDR}"
(
  cd "$ROOT_DIR"
  HACKME_COORDINATOR_ADDR="$COORD_ADDR" \
  HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN" \
  go run ./cmd/coordinator
) >"$LOG_DIR/coordinator.log" 2>&1 &
coord_pid=$!

echo "[stack] starting main node -> ${MAIN_ADDR}"
(
  cd "$ROOT_DIR"
  HACKME_BIND_ADDR="$MAIN_ADDR" \
  HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
  HACKME_POOL_COORDINATOR_URL="$COORD_BASE" \
  HACKME_POOL_COORDINATOR_TOKEN="$ADMIN_TOKEN" \
  go run .
) >"$LOG_DIR/main.log" 2>&1 &
main_pid=$!

wait_http_ok "coordinator" "${COORD_BASE}/api/network/stats"
wait_http_ok "main" "${MAIN_BASE}/api/status"

if [[ "$START_LOCAL_WORKER" == "1" ]]; then
  require_cmd jq
  echo "[stack] starting local worker id=${WORKER_ID}"
  (
    cd "$ROOT_DIR"
    COORD_URL="$COORD_BASE" \
    COORD_ADMIN_TOKEN="$ADMIN_TOKEN" \
    WORKER_ID="$WORKER_ID" \
    BATCH_SIZE="$WORKER_BATCH_SIZE" \
    HASHRATE_GHS="$WORKER_HASHRATE_GHS" \
    "$ROOT_DIR/scripts/ops/worker_loop.sh"
  ) >"$LOG_DIR/worker.log" 2>&1 &
  worker_pid=$!
fi

echo "[stack] up. logs:"
echo "  main:        $LOG_DIR/main.log"
echo "  coordinator: $LOG_DIR/coordinator.log"
if [[ -n "$worker_pid" ]]; then
  echo "  worker:      $LOG_DIR/worker.log"
fi
echo "[stack] monitoring (Ctrl+C to stop all)"

while true; do
  if ! kill -0 "$coord_pid" >/dev/null 2>&1; then
    echo "[stack] DOWN: coordinator process exited" >&2
    exit 1
  fi
  if ! kill -0 "$main_pid" >/dev/null 2>&1; then
    echo "[stack] DOWN: main process exited" >&2
    exit 1
  fi
  if [[ -n "$worker_pid" ]] && ! kill -0 "$worker_pid" >/dev/null 2>&1; then
    echo "[stack] DOWN: worker process exited" >&2
    exit 1
  fi

  if ! curl -fsS "${COORD_BASE}/api/network/stats" >/dev/null 2>&1; then
    echo "[stack] DEGRADED: coordinator health failed (${COORD_BASE}/api/network/stats)" >&2
  fi
  if ! curl -fsS "${MAIN_BASE}/api/status" >/dev/null 2>&1; then
    echo "[stack] DEGRADED: main health failed (${MAIN_BASE}/api/status)" >&2
  fi
  if ! curl -fsS "${MAIN_BASE}/api/network/stats" >/dev/null 2>&1; then
    echo "[stack] DEGRADED: network stats failed (${MAIN_BASE}/api/network/stats)" >&2
  fi
  if ! curl -fsS "${MAIN_BASE}/api/work/stats" >/dev/null 2>&1; then
    echo "[stack] DEGRADED: work stats failed (${MAIN_BASE}/api/work/stats)" >&2
  fi

  sleep 5
done

