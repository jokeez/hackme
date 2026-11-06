#!/usr/bin/env bash
# Desktop / app-menu launcher for apt + tarball installs.
# Always uses a user-writable XDG state dir when /opt/hackme is not writable (typical apt).
#
#   hackme_desktop_launch.sh           # start node + open dashboard
#   hackme_desktop_launch.sh --open    # ensure node up, then open browser only
#   hackme_desktop_launch.sh --start   # start node only (no browser)
set -euo pipefail

INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODE=start-open
case "${1:-}" in
  --open|--open-only) MODE=open; shift || true ;;
  --start|--start-only) MODE=start; shift || true ;;
  -h|--help)
    echo "Usage: $0 [--open|--start]"
    exit 0
    ;;
esac

BIN="${INSTALL_DIR}/hackme"
# Apt binary lives in /opt/hackme; allow running this script from a git checkout / XDG copy.
if [[ ! -x "$BIN" && -x /opt/hackme/hackme ]]; then
  BIN=/opt/hackme/hackme
fi
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
STATE_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/hackme"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/hackme"
LOG_DIR="${LOG_DIR:-$STATE_DIR/logs}"
DATA_DIR="${HACKME_DATA_DIR:-$STATE_DIR/data}"
ENV_FILE="${ENV_FILE:-$CONFIG_DIR/env}"
PID_FILE="${PID_FILE:-$LOG_DIR/hackme-node.pid}"
NODE_LOG="${NODE_LOG:-$LOG_DIR/hackme-node.log}"

notify() {
  local msg="$1"
  echo "[hackme-desktop] $msg" >&2
  if [[ ! -t 1 ]] && command -v notify-send >/dev/null 2>&1; then
    notify-send -a "HackMe" "HackMe" "$msg" 2>/dev/null || true
  fi
}

die() {
  notify "$1"
  if [[ ! -t 1 ]] && command -v zenity >/dev/null 2>&1; then
    zenity --error --title="HackMe" --width=420 --text="$1" 2>/dev/null || true
  fi
  exit 1
}

[[ -x "$BIN" ]] || die "hackme binary missing at $BIN — reinstall: sudo apt install --reinstall hackme-node"

mkdir -p "$STATE_DIR" "$CONFIG_DIR" "$LOG_DIR" "$DATA_DIR"
chmod 700 "$DATA_DIR" 2>/dev/null || true

pick_pool_token() {
  local c
  for c in \
    "${CONFIG_DIR}/pool.miner.token" \
    "${INSTALL_DIR}/pool.miner.token" \
    "${HOME}/Downloads/pool.miner.token" \
    "${HOME}/Загрузки/pool.miner.token"; do
    if [[ -f "$c" ]]; then
      local tok
      tok="$(tr -d '\r\n' <"$c" 2>/dev/null || true)"
      if [[ -n "$tok" && "$tok" != "REPLACE_WITH_POOL_TOKEN" ]]; then
        echo "$tok"
        return 0
      fi
    fi
  done
  echo ""
}

ensure_env() {
  local admin="" pool=""
  if [[ -f "$ENV_FILE" ]]; then
    admin="$(grep -E '^HACKME_ADMIN_TOKEN=' "$ENV_FILE" 2>/dev/null | head -n1 | cut -d= -f2- | tr -d '\r' || true)"
  fi
  if [[ -z "$admin" ]]; then
    if command -v openssl >/dev/null 2>&1; then
      admin="$(openssl rand -hex 24)"
    else
      admin="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    fi
  fi
  pool="$(pick_pool_token)"

  # Always rewrite a clean desktop env into the user config (never into /opt/hackme).
  cat >"$ENV_FILE" <<EOF
HACKME_BIND_ADDR=127.0.0.1:8080
HACKME_ADMIN_TOKEN=${admin}
HACKME_REQUIRE_ADMIN_TOKEN=1
HACKME_DESKTOP_MODE=1
HACKME_INTEGRATOR_SELF_REGISTER=1
HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech
HACKME_CANONICAL_CHAIN_URL=https://hackme.tech
HACKME_DATA_DIR=${DATA_DIR}
HACKME_WORKING_DIR=${STATE_DIR}
HACKME_WORKER_LOCK_DIR=${STATE_DIR}/logs
LOG_DIR=${STATE_DIR}/logs
HACKME_WORKER_WATCHDOG=1
WORKER_AUTOSTART=1
HACKME_WORKER_HEARTBEAT_STALE_SEC=180
HACKME_WORKER_HEARTBEAT_GRACE_SEC=120
HACKME_FUZZ_SETTLE_PULL=0
HACKME_DESKTOP_GPU_POOL=1
HACKME_POOL_DIRECT=1
HACKME_POOL_DIRECT_URL=http://132.243.112.100:18083
DESKTOP_PROFILE=worker
EOF
  if [[ -n "$pool" ]]; then
    printf 'HACKME_POOL_COORDINATOR_TOKEN=%s\n' "$pool" >>"$ENV_FILE"
    # Keep a copy under config for next launches
    printf '%s\n' "$pool" >"${CONFIG_DIR}/pool.miner.token"
    chmod 600 "${CONFIG_DIR}/pool.miner.token" 2>/dev/null || true
  fi
  chmod 600 "$ENV_FILE" 2>/dev/null || true
  # Seed files must be 0600 (node refuses group/other-readable seeds → mining start fails).
  chmod 700 "$DATA_DIR" 2>/dev/null || true
  for f in "${DATA_DIR}/node_ed25519.seed" "${DATA_DIR}/miner_submit_ed25519_seed.hex"; do
    [[ -f "$f" ]] || continue
    chmod 600 "$f" 2>/dev/null || true
  done
}

node_up() {
  curl -fsS --max-time 2 "$BASE_URL/api/status?lite=1" >/dev/null 2>&1 \
    || curl -fsS --max-time 2 "$BASE_URL/api/health" >/dev/null 2>&1 \
    || curl -fsS --max-time 2 "${BASE_URL%/}/" >/dev/null 2>&1
}

stop_stale_pid() {
  if [[ -f "$PID_FILE" ]]; then
    local old
    old="$(cat "$PID_FILE" 2>/dev/null || true)"
    if [[ -n "$old" ]] && kill -0 "$old" 2>/dev/null; then
      if node_up; then
        return 0
      fi
      kill "$old" 2>/dev/null || true
      sleep 1
    fi
    rm -f "$PID_FILE"
  fi
}

start_node() {
  ensure_env
  stop_stale_pid
  if node_up; then
    notify "Dashboard already running — $BASE_URL"
    return 0
  fi

  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
  export HACKME_DATA_DIR="$DATA_DIR"
  export HACKME_WORKING_DIR="$STATE_DIR"

  notify "Starting HackMe node…"
  nohup "$BIN" >"$NODE_LOG" 2>&1 &
  echo $! >"$PID_FILE"

  local i
  for i in $(seq 1 40); do
    if node_up; then
      break
    fi
    sleep 0.25
  done
  if ! node_up; then
    tail -n 40 "$NODE_LOG" >&2 || true
    if grep -q 'address already in use' "$NODE_LOG" 2>/dev/null; then
      die "Port 8080 is in use but is not responding as HackMe. Stop the other process or set HACKME_BIND_ADDR in $ENV_FILE."
    fi
    die "Node did not start. See log: $NODE_LOG"
  fi

  if [[ -z "${HACKME_POOL_COORDINATOR_TOKEN:-}" ]]; then
    notify "Node is up. Add pool.miner.token to $CONFIG_DIR to enable pool mining."
  else
    # Best-effort worker start (dashboard stays usable either way).
    local coord="https://hackme.tech/pool/coordinator"
    curl -fsS -X POST "$BASE_URL/api/worker/start" \
      -H "Content-Type: application/json" \
      -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
      -d "{\"coord_url\":\"${coord}\"}" >/dev/null 2>&1 || true
  fi
  notify "HackMe is running — $BASE_URL"
}

open_dashboard() {
  local url="${BASE_URL}/#ecosystem"
  if command -v xdg-open >/dev/null 2>&1; then
    xdg-open "$url" >/dev/null 2>&1 || /usr/bin/xdg-open "$url" >/dev/null 2>&1 || true
  elif command -v gio >/dev/null 2>&1; then
    gio open "$url" >/dev/null 2>&1 || true
  else
    notify "Open in browser: $url"
  fi
}

case "$MODE" in
  start)
    start_node
    ;;
  open)
    start_node
    open_dashboard
    ;;
  start-open)
    start_node
    open_dashboard
    ;;
esac
exit 0
