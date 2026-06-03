#!/usr/bin/env bash
# Start coordinator + HackMe node for Playwright E2E (loopback, insecure dev tokens).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG_DIR="${E2E_LOG_DIR:-$ROOT_DIR/logs/e2e}"
COORD_ADDR="${E2E_COORD_ADDR:-127.0.0.1:19081}"
NODE_ADDR="${E2E_NODE_ADDR:-127.0.0.1:19080}"
COORD_URL="http://${COORD_ADDR}"
NODE_URL="http://${NODE_ADDR}"
ADMIN_TOKEN="${E2E_ADMIN_TOKEN:-e2e-admin-token-test}"
WORKER_TOKEN="${E2E_WORKER_TOKEN:-e2e-worker-token-test}"

mkdir -p "$LOG_DIR"
COORD_BIN="${E2E_COORD_BIN:-$ROOT_DIR/bin/coordinator-e2e}"
NODE_BIN="${E2E_NODE_BIN:-$ROOT_DIR/bin/hackme-e2e}"

if [[ ! -x "$COORD_BIN" ]]; then
  echo "[e2e-stack] building coordinator..."
  (cd "$ROOT_DIR" && go build -trimpath -o "$COORD_BIN" ./cmd/coordinator)
fi
if [[ ! -x "$NODE_BIN" ]]; then
  echo "[e2e-stack] building node..."
  (cd "$ROOT_DIR" && go build -trimpath -o "$NODE_BIN" .)
fi

stop_pid() {
  local pid_file="$1"
  if [[ -f "$pid_file" ]]; then
    local pid
    pid="$(cat "$pid_file" 2>/dev/null || true)"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
    rm -f "$pid_file"
  fi
}

stop_pid "$LOG_DIR/coordinator.pid"
stop_pid "$LOG_DIR/node.pid"

export HACKME_COORDINATOR_ADDR="$COORD_ADDR"
export HACKME_COORDINATOR_DB="$LOG_DIR/coordinator.db"
export HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN"
export HACKME_COORDINATOR_WORKER_TOKEN="$WORKER_TOKEN"
export HACKME_COORDINATOR_ALLOW_INSECURE=1
export HACKME_COORDINATOR_REQUIRE_ADMIN_TOKEN=0
export HACKME_COORDINATOR_PAYOUT_FOUND_ONLY=0

nohup "$COORD_BIN" >"$LOG_DIR/coordinator.log" 2>&1 &
echo $! >"$LOG_DIR/coordinator.pid"

export HACKME_ADMIN_TOKEN="$ADMIN_TOKEN"
export HACKME_POOL_COORDINATOR_URL="$COORD_URL"
export HACKME_POOL_COORDINATOR_TOKEN="$ADMIN_TOKEN"
export HACKME_NETWORK_MOCK=0
export HACKME_BIND_ADDR="$NODE_ADDR"
export HACKME_DATA_DIR="$LOG_DIR/node-data"
mkdir -p "$HACKME_DATA_DIR"

nohup "$NODE_BIN" >"$LOG_DIR/node.log" 2>&1 &
echo $! >"$LOG_DIR/node.pid"

for _ in $(seq 1 60); do
  if curl -fsS "$COORD_URL/health" >/dev/null 2>&1 && curl -fsS "$NODE_URL/api/status" >/dev/null 2>&1; then
    curl -fsS -X POST -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" "$NODE_URL/api/genesis" >/dev/null 2>&1 || true
    echo "[e2e-stack] ready coord=$COORD_URL node=$NODE_URL"
    if [[ "${E2E_STACK_READY_ONLY:-0}" == "1" ]]; then
      exit 0
    fi
    # Playwright webServer: command must stay alive while tests run.
    trap 'stop_pid "$LOG_DIR/coordinator.pid"; stop_pid "$LOG_DIR/node.pid"' EXIT INT TERM
    coord_pid="$(cat "$LOG_DIR/coordinator.pid")"
    node_pid="$(cat "$LOG_DIR/node.pid")"
    wait "$coord_pid" "$node_pid" 2>/dev/null || tail -f /dev/null
    exit 0
  fi
  sleep 0.5
done

echo "[e2e-stack] FAILED — see $LOG_DIR/*.log" >&2
tail -30 "$LOG_DIR/coordinator.log" 2>/dev/null || true
tail -30 "$LOG_DIR/node.log" 2>/dev/null || true
exit 1
