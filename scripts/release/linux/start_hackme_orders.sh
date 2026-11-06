#!/usr/bin/env bash
# HackMe Orders (B2B) — local node for Dig / Hunt campaigns (no mining worker).
#
#   bash start_hackme_orders.sh           # start node + open #orders
#   bash start_hackme_orders.sh --open    # ensure up, open dashboard
#   bash start_hackme_orders.sh --start   # start node only
#
# Env lives in XDG config (writable even when /opt/hackme is root-owned apt install).
# CLI (optional): keep hackme-fuzzing next to this script or on PATH.
set -euo pipefail

INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODE=start-open
case "${1:-}" in
  --open|--open-only) MODE=open; shift || true ;;
  --start|--start-only) MODE=start; shift || true ;;
  -h|--help)
    echo "Usage: $0 [--open|--start]"
    echo "  B2B Orders profile — Dig/Hunt on 127.0.0.1:8080/#orders (no pool miner)."
    exit 0
    ;;
esac

BIN="${INSTALL_DIR}/hackme"
if [[ ! -x "$BIN" && -x /opt/hackme/hackme ]]; then
  BIN=/opt/hackme/hackme
fi
# HACKME_ORDERS_PORT runs the Orders node next to a miner node on the same host (miner keeps 8080).
ORDERS_PORT="${HACKME_ORDERS_PORT:-8080}"
BASE_URL_SET="${BASE_URL:+1}"
BASE_URL="${BASE_URL:-http://127.0.0.1:${ORDERS_PORT}}"
STATE_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/hackme"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/hackme"
LOG_DIR="${LOG_DIR:-$STATE_DIR/logs/orders}"
# Isolate Orders ledger from miner desktop data (same port 8080 clash otherwise).
DATA_DIR="${HACKME_DATA_DIR:-$STATE_DIR/data/orders}"
ENV_FILE="${ENV_FILE:-$CONFIG_DIR/env.orders}"
PID_FILE="${PID_FILE:-$LOG_DIR/hackme-orders.pid}"
NODE_LOG="${NODE_LOG:-$LOG_DIR/hackme-orders.log}"

DEFAULT_COORD_URL="${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}"

notify() {
  local msg="$1"
  echo "[hackme-orders] $msg" >&2
  if [[ ! -t 1 ]] && command -v notify-send >/dev/null 2>&1; then
    notify-send -a "HackMe Orders" "HackMe Orders" "$msg" 2>/dev/null || true
  fi
}

die() {
  notify "$1"
  if [[ ! -t 1 ]] && command -v zenity >/dev/null 2>&1; then
    zenity --error --title="HackMe Orders" --width=420 --text="$1" 2>/dev/null || true
  fi
  exit 1
}

[[ -x "$BIN" ]] || die "hackme binary missing at $BIN — install: sudo apt install hackme-node  (or extract the Linux tarball)"

mkdir -p "$STATE_DIR" "$CONFIG_DIR" "$LOG_DIR" "$DATA_DIR"
chmod 700 "$DATA_DIR" 2>/dev/null || true

read_orders_publish_token() {
  local tok="" f
  # Scoped publish only — never fall back to coordinator admin (rc17 leak class).
  for f in \
    "${INSTALL_DIR}/pool.orders.token" \
    "${CONFIG_DIR}/pool.orders.token" \
    "${INSTALL_DIR}/.secrets/hackme_coordinator_orders_publish_token"; do
    [[ -f "$f" ]] || continue
    tok="$(tr -d '\r\n' <"$f")"
    if [[ -n "$tok" && "$tok" != "REPLACE_WITH_ORDERS_PUBLISH_TOKEN" ]]; then
      printf '%s' "$tok"
      return 0
    fi
  done
  return 1
}

ensure_env() {
  local admin=""
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

  if [[ ! -f "$ENV_FILE" ]]; then
    cat >"$ENV_FILE" <<EOF
HACKME_BIND_ADDR=127.0.0.1:${ORDERS_PORT}
HACKME_ADMIN_TOKEN=${admin}
HACKME_REQUIRE_ADMIN_TOKEN=1
HACKME_DESKTOP_MODE=1
# EXPOSE=1 embeds admin_token in dashboard HTML. Leave off: loopback
# GET /api/desktop/local-auth already re-syncs the UI after restart.
HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=0
HACKME_INTEGRATOR_SELF_REGISTER=1
HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech
HACKME_CANONICAL_CHAIN_URL=https://hackme.tech
HACKME_POOL_COORDINATOR_URL=${DEFAULT_COORD_URL}
HACKME_DATA_DIR=${DATA_DIR}
HACKME_WORKING_DIR=${STATE_DIR}
HACKME_WORKER_LOCK_DIR=${STATE_DIR}/logs
LOG_DIR=${LOG_DIR}
DESKTOP_PROFILE=orders
HACKME_ORDERS_STARTER_HMC=0
WORKER_AUTOSTART=0
HACKME_WORKER_WATCHDOG=0
HACKME_DESKTOP_GPU_POOL=0
HACKME_FUZZ_SETTLE_PULL=1
EOF
  fi

  repair_pool_coordinator_env
  repair_orders_settle_pull
  chmod 600 "$ENV_FILE" 2>/dev/null || true
  chmod 700 "$DATA_DIR" 2>/dev/null || true
  mkdir -p "$DATA_DIR" 2>/dev/null || true
  for f in "${DATA_DIR}/node_ed25519.seed" "${DATA_DIR}/miner_submit_ed25519_seed.hex"; do
    [[ -f "$f" ]] || continue
    chmod 600 "$f" 2>/dev/null || true
  done
}

repair_pool_coordinator_env() {
  [[ -f "$ENV_FILE" ]] || return 0
  if ! grep -q '^HACKME_POOL_COORDINATOR_URL=.\+' "$ENV_FILE" 2>/dev/null; then
    if grep -q '^HACKME_POOL_COORDINATOR_URL=' "$ENV_FILE" 2>/dev/null; then
      sed -i "s|^HACKME_POOL_COORDINATOR_URL=.*|HACKME_POOL_COORDINATOR_URL=${DEFAULT_COORD_URL}|" "$ENV_FILE"
    else
      echo "HACKME_POOL_COORDINATOR_URL=${DEFAULT_COORD_URL}" >>"$ENV_FILE"
    fi
    echo "[hackme-orders] set HACKME_POOL_COORDINATOR_URL=${DEFAULT_COORD_URL}"
  fi
  if grep -q '^HACKME_POOL_ORDERS_PUBLISH_TOKEN=.\+' "$ENV_FILE" 2>/dev/null \
    || grep -q '^HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN=.\+' "$ENV_FILE" 2>/dev/null; then
    return 0
  fi
  local pub
  pub="$(read_orders_publish_token 2>/dev/null || true)"
  if [[ -z "$pub" ]]; then
    echo "[hackme-orders] NOTE: no pool.orders.token yet — local Dig/Hunt still work."
    echo "[hackme-orders] rc18.1+ Orders bundles ship a scoped publish token; or paste into:"
    echo "[hackme-orders]   ${INSTALL_DIR}/pool.orders.token  (or ${CONFIG_DIR}/pool.orders.token)"
    return 0
  fi
  if grep -q '^HACKME_POOL_ORDERS_PUBLISH_TOKEN=' "$ENV_FILE" 2>/dev/null; then
    sed -i "s|^HACKME_POOL_ORDERS_PUBLISH_TOKEN=.*|HACKME_POOL_ORDERS_PUBLISH_TOKEN=${pub}|" "$ENV_FILE"
  else
    echo "HACKME_POOL_ORDERS_PUBLISH_TOKEN=${pub}" >>"$ENV_FILE"
  fi
  if [[ -f "${INSTALL_DIR}/pool.orders.token" && ! -f "${CONFIG_DIR}/pool.orders.token" ]]; then
    cp "${INSTALL_DIR}/pool.orders.token" "${CONFIG_DIR}/pool.orders.token"
    chmod 600 "${CONFIG_DIR}/pool.orders.token" 2>/dev/null || true
  fi
  echo "[hackme-orders] set scoped pool publish token from pool.orders.token (not admin/miner)"
}

# Miner scripts seed settle-pull=0; Orders needs pull when publish/admin token is present
# so escrow can close after progress sync.
repair_orders_settle_pull() {
  [[ -f "$ENV_FILE" ]] || return 0
  if grep -q '^HACKME_POOL_ORDERS_PUBLISH_TOKEN=.\+' "$ENV_FILE" 2>/dev/null \
    || grep -q '^HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN=.\+' "$ENV_FILE" 2>/dev/null \
    || grep -q '^HACKME_POOL_COORDINATOR_ADMIN_TOKEN=.\+' "$ENV_FILE" 2>/dev/null \
    || grep -q '^HACKME_COORDINATOR_ADMIN_TOKEN=.\+' "$ENV_FILE" 2>/dev/null; then
    if grep -q '^HACKME_FUZZ_SETTLE_PULL=0$' "$ENV_FILE" 2>/dev/null \
      || grep -qi '^HACKME_FUZZ_SETTLE_PULL=false$' "$ENV_FILE" 2>/dev/null; then
      sed -i 's/^HACKME_FUZZ_SETTLE_PULL=.*/HACKME_FUZZ_SETTLE_PULL=1/' "$ENV_FILE"
      echo "[hackme-orders] enabled HACKME_FUZZ_SETTLE_PULL=1 (Orders publish/admin credential present)"
    elif ! grep -q '^HACKME_FUZZ_SETTLE_PULL=' "$ENV_FILE" 2>/dev/null; then
      echo 'HACKME_FUZZ_SETTLE_PULL=1' >>"$ENV_FILE"
      echo "[hackme-orders] set HACKME_FUZZ_SETTLE_PULL=1"
    fi
  fi
}

# Optional Dig from_code toolchains (portable under install or XDG).
ensure_toolchains() {
  if [[ "${SKIP_TOOLCHAINS:-0}" == "1" ]]; then
    return 0
  fi
  local tc=""
  for tc in \
    "${INSTALL_DIR}/install_from_code_toolchains.sh" \
    "${INSTALL_DIR}/../ops/install_from_code_toolchains.sh" \
    "/opt/hackme/install_from_code_toolchains.sh"; do
    if [[ -x "$tc" ]]; then
      APP_DIR="${INSTALL_DIR}" bash "$tc" >/dev/null 2>&1 || true
      break
    fi
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

print_cli_hint() {
  local cli=""
  for cli in \
    "${INSTALL_DIR}/hackme-fuzzing" \
    "${INSTALL_DIR}/bin/hackme-fuzzing" \
    "$(command -v hackme-fuzzing 2>/dev/null || true)"; do
    if [[ -n "$cli" && -x "$cli" ]]; then
      echo "[hackme-orders] CLI: $cli wizard --pack secrets --package audit" >&2
      echo "[hackme-orders] Hunt: $cli hunt packages" >&2
      return 0
    fi
  done
  echo "[hackme-orders] CLI tip: download hackme-fuzzing from https://hackme.tech/downloads.html#fuzzing-client" >&2
  echo "[hackme-orders]        or place it next to hackme in ${INSTALL_DIR}" >&2
}

# Port from env.orders (HACKME_BIND_ADDR), overridden by HACKME_ORDERS_PORT; keeps BASE_URL in step.
sync_bind_addr() {
  local bind
  bind="$(grep -E '^HACKME_BIND_ADDR=' "$ENV_FILE" 2>/dev/null | tail -n1 | cut -d= -f2- | tr -d '\r' || true)"
  if [[ -n "${HACKME_ORDERS_PORT:-}" ]]; then
    bind="127.0.0.1:${HACKME_ORDERS_PORT}"
    if grep -q '^HACKME_BIND_ADDR=' "$ENV_FILE" 2>/dev/null; then
      sed -i "s|^HACKME_BIND_ADDR=.*|HACKME_BIND_ADDR=${bind}|" "$ENV_FILE"
    else
      echo "HACKME_BIND_ADDR=${bind}" >>"$ENV_FILE"
    fi
  fi
  if [[ -n "$bind" && -z "$BASE_URL_SET" ]]; then
    BASE_URL="http://${bind}"
  fi
}
orders_pid_alive() {
  local p
  p="$(cat "$PID_FILE" 2>/dev/null || true)"
  [[ -n "$p" ]] && kill -0 "$p" 2>/dev/null
}
start_node() {
  ensure_toolchains
  ensure_env
  sync_bind_addr
  stop_stale_pid
  if node_up; then
    if orders_pid_alive; then
      notify "Node already running — ${BASE_URL}/#orders"
      return 0
    fi
    # Something else answers on this port — usually the Miner/desktop node, which has no Orders
    # credentials. Do not open it as if it were the Orders node.
    die "Port ${BASE_URL##*:} is used by another HackMe node (Miner/desktop?). Stop it, or run Orders on its own port: HACKME_ORDERS_PORT=8081 bash $0"
  fi

  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
  export HACKME_DATA_DIR="$DATA_DIR"
  export HACKME_WORKING_DIR="$STATE_DIR"
  export HACKME_REPO_ROOT="${HACKME_REPO_ROOT:-$INSTALL_DIR}"

  notify "Starting HackMe Orders node…"
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
      die "Port ${BASE_URL##*:} is in use. Stop Miner/desktop first, or run with HACKME_ORDERS_PORT=8081."
    fi
    die "Node did not start. See log: $NODE_LOG"
  fi
  notify "Orders ready — ${BASE_URL}/#orders"
  print_cli_hint
}

open_orders() {
  local url="${BASE_URL}/#orders"
  if command -v xdg-open >/dev/null 2>&1; then
    xdg-open "$url" >/dev/null 2>&1 || /usr/bin/xdg-open "$url" >/dev/null 2>&1 || true
  elif command -v gio >/dev/null 2>&1; then
    gio open "$url" >/dev/null 2>&1 || true
  else
    notify "Open in browser: $url"
  fi
}

case "$MODE" in
  start) start_node ;;
  open) start_node; open_orders ;;
  start-open) start_node; open_orders ;;
esac
exit 0
