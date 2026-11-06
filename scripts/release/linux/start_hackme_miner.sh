#!/usr/bin/env bash
# Public-pool miner — extract tarball, run this script. No manual token or pool URL.
# Apt installs: /opt/hackme is often root-owned — delegate to hackme_desktop_launch.sh
# which keeps state under ~/.local/share/hackme.
set -euo pipefail

INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$INSTALL_DIR"

# Menu / apt path: not writable install dir → XDG desktop launcher.
if [[ ! -w "$INSTALL_DIR" && -x "$INSTALL_DIR/hackme_desktop_launch.sh" ]]; then
  exec "$INSTALL_DIR/hackme_desktop_launch.sh" "$@"
fi

ENV_FILE="${ENV_FILE:-$INSTALL_DIR/.env}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
LOG_DIR="${LOG_DIR:-$INSTALL_DIR/logs}"
PID_FILE="$LOG_DIR/hackme-node.pid"
NODE_LOG="$LOG_DIR/hackme-node.log"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[miner] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl

if [[ ! -x "$INSTALL_DIR/hackme" ]]; then
  echo "[miner] hackme binary missing — extract the linux/ folder from the release tarball" >&2
  exit 1
fi

if [[ ! -w "$INSTALL_DIR" ]]; then
  echo "[miner] $INSTALL_DIR is not writable by $(id -un)." >&2
  echo "[miner] Use: bash $INSTALL_DIR/hackme_desktop_launch.sh" >&2
  echo "[miner] Or: sudo chown -R $(id -un):$(id -gn) $INSTALL_DIR/data $INSTALL_DIR/logs && place .env here" >&2
  exit 1
fi

if [[ ! -f "$ENV_FILE" ]]; then
  echo "[miner] first run — configuring pool access..."
  if [[ -f "$INSTALL_DIR/pool.miner.token" ]]; then
    bash "$INSTALL_DIR/setup_hackme_miner.sh"
  elif [[ -x "$INSTALL_DIR/hackme_desktop_launch.sh" ]]; then
    echo "[miner] no pool.miner.token — starting dashboard via desktop launcher (mining optional)" >&2
    exec "$INSTALL_DIR/hackme_desktop_launch.sh"
  else
    echo "[miner] pool.miner.token missing — download from https://hackme.tech/downloads.html" >&2
    exit 1
  fi
fi

if [[ ! -f "$INSTALL_DIR/pool.miner.token" ]]; then
  # Allow dashboard-only if env already has no pool token requirement.
  if ! grep -q '^HACKME_POOL_COORDINATOR_TOKEN=.\+' "$ENV_FILE" 2>/dev/null; then
    if [[ -x "$INSTALL_DIR/hackme_desktop_launch.sh" ]]; then
      exec "$INSTALL_DIR/hackme_desktop_launch.sh"
    fi
    echo "[miner] pool.miner.token missing — download from https://hackme.tech/downloads.html" >&2
    exit 1
  fi
fi

# Starting the miner always clears pause + restores watchdog (stop_hackme_miner / stop_pool_workers set them).
# Opt out: KEEP_MINING_PAUSED=1 bash start_hackme_miner.sh
if [[ "${KEEP_MINING_PAUSED:-0}" != "1" ]]; then
  if [[ -x "$INSTALL_DIR/scripts/ops/resume_pool_mining.sh" ]]; then
    LOG_DIR="$LOG_DIR" ROOT_DIR="$INSTALL_DIR" ENV_FILE="$ENV_FILE" \
      bash "$INSTALL_DIR/scripts/ops/resume_pool_mining.sh" >/dev/null || true
  elif [[ -x "$INSTALL_DIR/resume_pool_mining.sh" ]]; then
    LOG_DIR="$LOG_DIR" ROOT_DIR="$INSTALL_DIR" ENV_FILE="$ENV_FILE" \
      bash "$INSTALL_DIR/resume_pool_mining.sh" >/dev/null || true
  else
    rm -f "$LOG_DIR/mining_paused" 2>/dev/null || true
    if [[ -f "$ENV_FILE" ]]; then
      for key in HACKME_WORKER_WATCHDOG WORKER_AUTOSTART; do
        if grep -q "^${key}=" "$ENV_FILE" 2>/dev/null; then
          sed -i "s/^${key}=.*/${key}=1/" "$ENV_FILE"
        else
          echo "${key}=1" >>"$ENV_FILE"
        fi
      done
    fi
  fi
fi

# Repair empty/missing admin token (parity with Windows start_hackme_miner.bat).
repair_admin_token() {
  local admin=""
  if [[ -f "$ENV_FILE" ]]; then
    admin="$(grep -E '^HACKME_ADMIN_TOKEN=' "$ENV_FILE" | head -n1 | cut -d= -f2- | tr -d '\r' || true)"
  fi
  if [[ -n "$admin" ]]; then
    return 0
  fi
  if command -v openssl >/dev/null 2>&1; then
    admin="$(openssl rand -hex 24)"
  else
    admin="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  fi
  if grep -q '^HACKME_ADMIN_TOKEN=' "$ENV_FILE" 2>/dev/null; then
    sed -i "s/^HACKME_ADMIN_TOKEN=.*/HACKME_ADMIN_TOKEN=${admin}/" "$ENV_FILE"
  else
    echo "HACKME_ADMIN_TOKEN=${admin}" >>"$ENV_FILE"
  fi
  echo "[miner] repaired empty HACKME_ADMIN_TOKEN in $ENV_FILE"
}

# Repair pool token from pool.miner.token when .env is empty/broken.
repair_pool_token() {
  local pool_file="$INSTALL_DIR/pool.miner.token"
  [[ -f "$pool_file" ]] || return 0
  local tok
  tok="$(tr -d '\r\n' <"$pool_file")"
  [[ -n "$tok" && "$tok" != "REPLACE_WITH_POOL_TOKEN" ]] || return 0
  if grep -q '^HACKME_POOL_COORDINATOR_TOKEN=.\+' "$ENV_FILE" 2>/dev/null; then
    return 0
  fi
  if grep -q '^HACKME_POOL_COORDINATOR_TOKEN=' "$ENV_FILE" 2>/dev/null; then
    sed -i "s/^HACKME_POOL_COORDINATOR_TOKEN=.*/HACKME_POOL_COORDINATOR_TOKEN=${tok}/" "$ENV_FILE"
  else
    echo "HACKME_POOL_COORDINATOR_TOKEN=${tok}" >>"$ENV_FILE"
  fi
  echo "[miner] repaired HACKME_POOL_COORDINATOR_TOKEN from pool.miner.token"
}

# Ensure settle pull stays off for worker-token desktops.
ensure_settle_pull_off() {
  [[ -f "$ENV_FILE" ]] || return 0
  if grep -q '^HACKME_FUZZ_SETTLE_PULL=' "$ENV_FILE" 2>/dev/null; then
    sed -i 's/^HACKME_FUZZ_SETTLE_PULL=.*/HACKME_FUZZ_SETTLE_PULL=0/' "$ENV_FILE"
  else
    echo 'HACKME_FUZZ_SETTLE_PULL=0' >>"$ENV_FILE"
  fi
}

repair_admin_token
repair_pool_token
ensure_settle_pull_off

# Tighten seed files after zip/tar extract (0644/0666 breaks hybrid mining: "permissions too open").
repair_seed_perms() {
  local d="${HACKME_DATA_DIR:-$INSTALL_DIR/data}"
  mkdir -p "$d" 2>/dev/null || true
  chmod 700 "$d" 2>/dev/null || true
  for f in "$d/node_ed25519.seed" "$d/miner_submit_ed25519_seed.hex"; do
    [[ -f "$f" ]] || continue
    chmod 600 "$f" 2>/dev/null || true
  done
}
repair_seed_perms

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a
# HACKME_PORT (Docker image, or a second node on one host) wins over .env's HACKME_BIND_ADDR:
# sourcing .env above used to reset the bind to 8080, so the node collided with a host node.
if [[ -n "${HACKME_PORT:-}" ]]; then
  [[ "$HACKME_PORT" =~ ^[0-9]{2,5}$ ]] || { echo "[miner] HACKME_PORT must be a port number" >&2; exit 1; }
  export HACKME_BIND_ADDR="127.0.0.1:${HACKME_PORT}"
  BASE_URL="http://127.0.0.1:${HACKME_PORT}"
fi

# Hunt engines PATH (clang/AFL++) when install_hunt_engines.sh ran on this prefix.
for hunt_env in "$INSTALL_DIR/.env.hunt-engines" \
                "$INSTALL_DIR/toolchains/.env.hunt-engines" \
                "${HOME}/.local/share/hackme/toolchains/.env.hunt-engines"; do
  if [[ -f "$hunt_env" ]]; then
    set -a
    # shellcheck disable=SC1090
    . "$hunt_env"
    set +a
    export PATH
    break
  fi
done
# Best-effort first-boot engine ensure (skip with HACKME_SKIP_HUNT_ENGINES=1).
if [[ "${HACKME_SKIP_HUNT_ENGINES:-0}" != "1" && -x "$INSTALL_DIR/install_hunt_engines.sh" ]]; then
  if [[ ! -x "${HACKME_AFL_FUZZ:-}" ]] && ! command -v afl-fuzz >/dev/null 2>&1; then
    echo "[miner] ensuring Hunt engines (first missing AFL++)..."
    HACKME_PREFIX="$INSTALL_DIR" bash "$INSTALL_DIR/install_hunt_engines.sh" --prefix "$INSTALL_DIR" \
      || echo "[miner] WARN: Hunt engines incomplete — coverage Hunt soft-skips" >&2
    if [[ -f "$INSTALL_DIR/.env.hunt-engines" ]]; then
      _hackme_prev_path="$PATH"
      set -a
      # shellcheck disable=SC1090
      . "$INSTALL_DIR/.env.hunt-engines"
      set +a
      # env file carries a literal system PATH (systemd-safe); keep the caller's dirs too.
      PATH="$PATH:$_hackme_prev_path"
      export PATH
    fi
  fi
fi

# Force-enable after sourcing (stop may have left =0 in the file we already rewrote;
# re-export so the running process sees 1 even if sed raced).
export HACKME_WORKER_WATCHDOG="${HACKME_WORKER_WATCHDOG:-1}"
if [[ "${HACKME_WORKER_WATCHDOG}" == "0" && "${KEEP_MINING_PAUSED:-0}" != "1" ]]; then
  export HACKME_WORKER_WATCHDOG=1
fi
export WORKER_AUTOSTART="${WORKER_AUTOSTART:-1}"
if [[ "${WORKER_AUTOSTART}" == "0" && "${KEEP_MINING_PAUSED:-0}" != "1" ]]; then
  export WORKER_AUTOSTART=1
fi
export HACKME_FUZZ_SETTLE_PULL=0

if [[ -f "$LOG_DIR/mining_paused" && "${KEEP_MINING_PAUSED:-0}" == "1" ]]; then
  echo "[miner] mining paused (KEEP_MINING_PAUSED=1) — clear with: bash resume_pool_mining.sh" >&2
  exit 0
fi

if [[ -z "${HACKME_POOL_COORDINATOR_TOKEN:-}" ]]; then
  export HACKME_POOL_COORDINATOR_TOKEN="$(tr -d '\r\n' <"$INSTALL_DIR/pool.miner.token")"
fi
export HACKME_DATA_DIR="${HACKME_DATA_DIR:-$INSTALL_DIR/data}"
export HACKME_DESKTOP_MODE="${HACKME_DESKTOP_MODE:-1}"
export HACKME_WORKER_WATCHDOG="${HACKME_WORKER_WATCHDOG:-1}"
mkdir -p "$LOG_DIR" "$HACKME_DATA_DIR"

stop_node() {
  if [[ -f "$PID_FILE" ]]; then
    local old_pid
    old_pid="$(cat "$PID_FILE" 2>/dev/null || true)"
    if [[ -n "$old_pid" ]] && kill -0 "$old_pid" 2>/dev/null; then
      kill "$old_pid" 2>/dev/null || true
      sleep 1
    fi
    rm -f "$PID_FILE"
  fi
}

if [[ -f "$PID_FILE" ]]; then
  old_pid="$(cat "$PID_FILE" 2>/dev/null || true)"
  if [[ -n "$old_pid" ]] && kill -0 "$old_pid" 2>/dev/null; then
    if curl -fsS "$BASE_URL/api/status" >/dev/null 2>&1; then
      echo "[miner] node already running (pid=$old_pid) — $BASE_URL"
    else
      stop_node
    fi
  else
    rm -f "$PID_FILE"
  fi
fi

start_worker() {
  local coord_url resp http_code
  coord_url="$(curl -fsS "$BASE_URL/api/status" 2>/dev/null | python3 -c '
import json,sys
d=json.load(sys.stdin)
print((d.get("pool_coordinator_url_effective") or d.get("pool_coordinator_url") or "").strip())
' 2>/dev/null || true)"
  [[ -n "$coord_url" ]] || coord_url="https://hackme.tech/pool/coordinator"
  if [[ -z "${HACKME_ADMIN_TOKEN:-}" ]]; then
    echo "[miner] WARN: HACKME_ADMIN_TOKEN empty — cannot start workers" >&2
    return 1
  fi
  resp="$(mktemp)"
  http_code="$(curl -sS -o "$resp" -w '%{http_code}' -X POST "$BASE_URL/api/worker/start" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
    -d "{\"coord_url\":\"${coord_url}\"}" 2>/dev/null || echo "000")"
  if [[ "$http_code" != "200" ]]; then
    echo "[miner] WARN: worker/start HTTP ${http_code}: $(head -c 240 "$resp" 2>/dev/null || true)" >&2
    rm -f "$resp"
    return 1
  fi
  rm -f "$resp"
  echo "[miner] workers started (coord=${coord_url})"
}

if [[ ! -f "$PID_FILE" ]]; then
  echo "[miner] starting HackMe node (pool: hackme.tech)..."
  nohup "$INSTALL_DIR/hackme" >"$NODE_LOG" 2>&1 &
  echo "$!" >"$PID_FILE"
fi

for _ in $(seq 1 60); do
  if curl -fsS "$BASE_URL/api/status" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

if ! curl -fsS "$BASE_URL/api/status" >/dev/null 2>&1; then
  echo "[miner] node did not start — see $NODE_LOG" >&2
  tail -n 30 "$NODE_LOG" 2>/dev/null || true
  exit 1
fi

start_worker || true

if command -v xdg-open >/dev/null 2>&1; then
  (sleep 2; xdg-open "$BASE_URL/#ecosystem" >/dev/null 2>&1 &) || true
fi

echo ""
echo "HackMe miner is running."
echo "  Dashboard: $BASE_URL"
echo "  Logs:      $NODE_LOG"
echo "  Stop:      bash stop_hackme_miner.sh"
echo ""

# Default: daemon mode — closing the terminal must not kill mining.
# Opt into foreground follow with: HACKME_MINER_DAEMON=0 bash start_hackme_miner.sh
if [[ "${HACKME_MINER_DAEMON:-1}" == "1" ]]; then
  echo "Running in background (HACKME_MINER_DAEMON=1). Follow logs: tail -f $NODE_LOG"
  exit 0
fi

echo "Keep this terminal open, or run: tail -f $NODE_LOG"
echo ""
tail -f "$NODE_LOG"
