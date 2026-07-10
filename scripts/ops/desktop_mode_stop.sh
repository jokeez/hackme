#!/usr/bin/env bash
set -euo pipefail

# Full desktop stop: workers + node + port 8080. Prevents watchdog/autostart restart.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG_DIR="${LOG_DIR:-$ROOT_DIR/logs/desktop}"
PID_FILE="$LOG_DIR/node.pid"
DESKTOP_ENV_FILE="${DESKTOP_ENV_FILE:-$ROOT_DIR/.env.desktop}"
ENV_FILE="${ENV_FILE:-$ROOT_DIR/.env}"

bind_port() {
  local bind="${1:-127.0.0.1:8080}"
  printf '%s' "${bind##*:}"
}

set -a
# shellcheck disable=SC1090
[[ -f "$DESKTOP_ENV_FILE" ]] && . "$DESKTOP_ENV_FILE"
[[ -f "$ENV_FILE" ]] && . "$ENV_FILE"
set +a
PORT="$(bind_port "${HACKME_BIND_ADDR:-127.0.0.1:8080}")"

echo "[desktop-stop] stopping pool workers (autostart + workerpoh + API)..."
LOG_DIR="$LOG_DIR" ROOT_DIR="$ROOT_DIR" DESKTOP_ENV_FILE="$DESKTOP_ENV_FILE" ENV_FILE="$ENV_FILE" \
  bash "$ROOT_DIR/scripts/ops/stop_pool_workers.sh" || true

stop_systemd_desktop() {
  local unit user_unit="$HOME/.config/systemd/user/hackme-desktop.service"
  if [[ -f "$user_unit" ]] && grep -q '^Restart=always' "$user_unit" 2>/dev/null; then
    sed -i 's/^Restart=always/Restart=on-failure/' "$user_unit"
    systemctl --user daemon-reload 2>/dev/null || true
    echo "[desktop-stop] patched $user_unit Restart=always → on-failure"
  fi
  local unit
  for unit in hackme-desktop.service hackme-worker-autostart.service; do
    if systemctl --user is-active "$unit" >/dev/null 2>&1; then
      echo "[desktop-stop] systemctl --user stop $unit"
      systemctl --user stop "$unit" || true
    fi
    if systemctl is-active "$unit" >/dev/null 2>&1; then
      echo "[desktop-stop] sudo systemctl stop $unit"
      sudo systemctl stop "$unit" 2>/dev/null || true
    fi
  done
}
stop_systemd_desktop

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
else
  pid="$(cat "$PID_FILE" 2>/dev/null || true)"
  if [[ -z "$pid" ]]; then
    rm -f "$PID_FILE"
    echo "[desktop-stop] empty pid file removed"
  elif kill -0 "$pid" >/dev/null 2>&1; then
    kill "$pid" >/dev/null 2>&1 || true
    sleep 0.7
    if kill -0 "$pid" >/dev/null 2>&1; then
      kill -9 "$pid" >/dev/null 2>&1 || true
    fi
    echo "[desktop-stop] stopped node pid=$pid"
    rm -f "$PID_FILE"
  else
    echo "[desktop-stop] pid $pid not running (stale pid file — common after old go run)"
    rm -f "$PID_FILE"
  fi
fi

kill_port_listeners "$PORT"

if ss -lntp 2>/dev/null | grep -q ":${PORT} "; then
  echo "[desktop-stop] WARN port ${PORT} still in use — check: ss -lntp | grep ${PORT}"
else
  echo "[desktop-stop] OK port ${PORT} free"
fi

if pgrep -af 'workerpoh|worker_autostart|worker_loop' 2>/dev/null | grep -vE 'desktop_mode_stop|pgrep|grep'; then
  echo "[desktop-stop] WARN worker processes still present" >&2
  exit 1
fi
echo "[desktop-stop] OK — mining paused (resume: bash scripts/ops/resume_pool_mining.sh)"
