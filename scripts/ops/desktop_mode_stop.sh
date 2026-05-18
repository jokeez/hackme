#!/usr/bin/env bash
set -euo pipefail

# Stops the desktop node: PID from logs/desktop/node.pid, then frees the bind port
# (needed because older desktop_mode_up used `go run` and the saved PID was not the server).

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG_DIR="${LOG_DIR:-$ROOT_DIR/logs/desktop}"
PID_FILE="$LOG_DIR/node.pid"
DESKTOP_ENV_FILE="${DESKTOP_ENV_FILE:-$ROOT_DIR/.env.desktop}"

bind_port() {
  local bind="${1:-127.0.0.1:8080}"
  printf '%s' "${bind##*:}"
}

if [[ -f "$DESKTOP_ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  . "$DESKTOP_ENV_FILE"
  set +a
fi
PORT="$(bind_port "${HACKME_BIND_ADDR:-127.0.0.1:8080}")"

kill_port_listeners() {
  local port="$1"
  if command -v fuser >/dev/null 2>&1; then
    if fuser "${port}/tcp" >/dev/null 2>&1; then
      echo "[desktop-stop] freeing port ${port}/tcp (fuser)"
      fuser -k -TERM "${port}/tcp" >/dev/null 2>&1 || true
      sleep 0.4
      fuser -k -KILL "${port}/tcp" >/dev/null 2>&1 || true
    fi
  fi
}

if [[ ! -f "$PID_FILE" ]]; then
  echo "[desktop-stop] no pid file; try port ${PORT} anyway"
  kill_port_listeners "$PORT"
  exit 0
fi

pid="$(cat "$PID_FILE" 2>/dev/null || true)"
if [[ -z "$pid" ]]; then
  rm -f "$PID_FILE"
  echo "[desktop-stop] empty pid file removed"
  kill_port_listeners "$PORT"
  exit 0
fi

stopped=0
if kill -0 "$pid" >/dev/null 2>&1; then
  kill "$pid" >/dev/null 2>&1 || true
  sleep 0.7
  if kill -0 "$pid" >/dev/null 2>&1; then
    kill -9 "$pid" >/dev/null 2>&1 || true
  fi
  echo "[desktop-stop] stopped node pid=$pid"
  stopped=1
else
  echo "[desktop-stop] pid $pid not running (stale pid file — common after old go run)"
fi

rm -f "$PID_FILE"

# Always try port cleanup: real server may differ from recorded pid (legacy go run).
kill_port_listeners "$PORT"

if [[ "$stopped" == "0" ]]; then
  echo "[desktop-stop] if node still runs, check: ss -lntp | grep ':${PORT}'"
fi
