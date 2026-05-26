#!/usr/bin/env bash
set -euo pipefail

# HackMe Desktop Mode:
# - Creates/loads .env.desktop (with generated admin token if missing)
# - Starts local node in background (dashboard + API = full UI)
# - Opens dashboard as an app-like desktop window when possible
#
# Profiles (DESKTOP_PROFILE):
#   command — full local chain host: genesis + local PoH from UI (no pool env). Default.
#   worker  — pool participant: set HACKME_POOL_COORDINATOR_URL / TOKEN (or COORD_URL).
#
# Listen on all interfaces (LAN): BIND_ADDR=0.0.0.0:8080 BASE_URL=http://127.0.0.1:8080 bash scripts/ops/desktop_mode_up.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[desktop-up] missing command: $1" >&2
    exit 1
  }
}

require_cmd go
require_cmd curl

DESKTOP_ENV_FILE="${DESKTOP_ENV_FILE:-$ROOT_DIR/.env.desktop}"
LOG_DIR="${LOG_DIR:-$ROOT_DIR/logs/desktop}"
PID_FILE="$LOG_DIR/node.pid"
NODE_LOG_FILE="$LOG_DIR/node.log"
BIND_ADDR="${BIND_ADDR:-127.0.0.1:8080}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
DESKTOP_PROFILE="${DESKTOP_PROFILE:-worker}" # worker = public pool (default); command = dedicated chain leader only
COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
SECRET_COORD_TOKEN_FILE="${SECRET_COORD_TOKEN_FILE:-$ROOT_DIR/.secrets/hackme_coordinator_admin_token}"
SECRET_ADMIN_FILE="${SECRET_ADMIN_FILE:-$ROOT_DIR/.secrets/hackme_admin_token}"
WORKER_AUTOSTART="${WORKER_AUTOSTART:-0}"
NODE_BIN="$LOG_DIR/hackme-node-desktop"

mkdir -p "$LOG_DIR"

# from_code compilers (zig, asc, tinygo, wat2wasm, rustup user) — idempotent, skip with SKIP_TOOLCHAINS=1
# Sets TOOLCHAINS_INSTALLED=1 when a full install run completed (may need node restart).
ensure_from_code_toolchains() {
  TOOLCHAINS_INSTALLED=0
  if [[ "${SKIP_TOOLCHAINS:-0}" == "1" ]]; then
    return 0
  fi
  local tc_script="$ROOT_DIR/scripts/ops/install_from_code_toolchains.sh"
  if [[ ! -f "$tc_script" ]]; then
    return 0
  fi
  if bash "$tc_script" --desktop --check-only 2>/dev/null; then
    return 0
  fi
  echo "[desktop-up] installing from_code toolchains (first run may download)..."
  if bash "$tc_script" --desktop; then
    TOOLCHAINS_INSTALLED=1
  else
    echo "[desktop-up] WARN: toolchain install incomplete — Orders from_code may fail for some languages" >&2
  fi
}

sync_toolchain_env_to_log_dir() {
  local src
  for src in "$ROOT_DIR/.hackme-toolchains.env" "$LOG_DIR/toolchains/.env.toolchains"; do
    if [[ -f "$src" ]]; then
      cp -f "$src" "$LOG_DIR/.hackme-toolchains.env"
      return 0
    fi
  done
}

source_toolchain_env() {
  local f
  for f in "$ROOT_DIR/.hackme-toolchains.env" "$LOG_DIR/toolchains/.env.toolchains"; do
    if [[ -f "$f" ]]; then
      set -a
      # shellcheck disable=SC1090
      . "$f"
      set +a
      export PATH
      return 0
    fi
  done
}

generate_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
    return
  fi
  python3 - <<'PY'
import secrets
print(secrets.token_hex(24))
PY
}

write_default_env() {
  local tok="$1"
  cat >"$DESKTOP_ENV_FILE" <<EOF
HACKME_BIND_ADDR=$BIND_ADDR
HACKME_ADMIN_TOKEN=$tok
HACKME_REQUIRE_ADMIN_TOKEN=1
HACKME_DESKTOP_MODE=1
DESKTOP_PROFILE=worker
HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech
HACKME_CANONICAL_CHAIN_URL=https://hackme.tech
# Pool: add HACKME_POOL_COORDINATOR_TOKEN=... from operator (or .secrets/hackme_coordinator_admin_token in dev tree)
EOF
  chmod 600 "$DESKTOP_ENV_FILE" || true
}

if [[ ! -f "$DESKTOP_ENV_FILE" ]]; then
  tok="$(generate_token)"
  write_default_env "$tok"
  echo "[desktop-up] created $DESKTOP_ENV_FILE with generated admin token"
fi

set -a
# shellcheck disable=SC1090
. "$DESKTOP_ENV_FILE"
set +a

# Removed env vars must not crash the node if still present in an old .env.desktop.
unset HACKME_BEGINNER_SOLO HACKME_ALLOW_LOCAL_SOLO 2>/dev/null || true

if [[ -f "$SECRET_ADMIN_FILE" ]]; then
  export HACKME_ADMIN_TOKEN="$(head -n1 "$SECRET_ADMIN_FILE" | tr -d '\r\n' | tr -d ' ')"
  if grep -q '^HACKME_ADMIN_TOKEN=' "$DESKTOP_ENV_FILE" 2>/dev/null; then
    sed -i "s|^HACKME_ADMIN_TOKEN=.*|HACKME_ADMIN_TOKEN=${HACKME_ADMIN_TOKEN}|" "$DESKTOP_ENV_FILE"
  else
    echo "HACKME_ADMIN_TOKEN=${HACKME_ADMIN_TOKEN}" >>"$DESKTOP_ENV_FILE"
  fi
  echo "[desktop-up] HACKME_ADMIN_TOKEN synced from $SECRET_ADMIN_FILE"
elif [[ -z "${HACKME_ADMIN_TOKEN:-}" ]]; then
  tok="$(generate_token)"
  {
    echo "HACKME_ADMIN_TOKEN=$tok"
  } >>"$DESKTOP_ENV_FILE"
  export HACKME_ADMIN_TOKEN="$tok"
  echo "[desktop-up] admin token was missing; generated and saved to $DESKTOP_ENV_FILE"
fi

if [[ -z "${HACKME_POOL_COORDINATOR_TOKEN:-}" && -f "$SECRET_COORD_TOKEN_FILE" ]]; then
  export HACKME_POOL_COORDINATOR_TOKEN="$(tr -d '\r\n' <"$SECRET_COORD_TOKEN_FILE")"
  echo "[desktop-up] HACKME_POOL_COORDINATOR_TOKEN loaded from $SECRET_COORD_TOKEN_FILE"
fi
if [[ -n "${HACKME_POOL_COORDINATOR_TOKEN:-}" ]]; then
  export HACKME_COORDINATOR_ADMIN_TOKEN="${HACKME_COORDINATOR_ADMIN_TOKEN:-$HACKME_POOL_COORDINATOR_TOKEN}"
  if grep -q '^HACKME_COORDINATOR_ADMIN_TOKEN=' "$DESKTOP_ENV_FILE" 2>/dev/null; then
    sed -i "s|^HACKME_COORDINATOR_ADMIN_TOKEN=.*|HACKME_COORDINATOR_ADMIN_TOKEN=${HACKME_COORDINATOR_ADMIN_TOKEN}|" "$DESKTOP_ENV_FILE"
  else
    echo "HACKME_COORDINATOR_ADMIN_TOKEN=${HACKME_COORDINATOR_ADMIN_TOKEN}" >>"$DESKTOP_ENV_FILE"
  fi
fi
if [[ "${DESKTOP_PROFILE}" == "worker" && -z "${HACKME_POOL_COORDINATOR_URL:-}" && -z "${HACKME_PUBLIC_AUTHORITY_BASE:-}" ]]; then
  export HACKME_POOL_COORDINATOR_URL="${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}"
  if ! grep -q '^HACKME_POOL_COORDINATOR_URL=' "$DESKTOP_ENV_FILE" 2>/dev/null; then
    echo "HACKME_POOL_COORDINATOR_URL=${HACKME_POOL_COORDINATOR_URL}" >>"$DESKTOP_ENV_FILE"
  fi
fi

# Relay signed transfers to hackme.tech (forwards admin token on POST /api/tx/send).
if [[ -z "${HACKME_CANONICAL_RELAY_ADMIN_TOKEN:-}" ]]; then
  export HACKME_CANONICAL_RELAY_ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-}"
fi
if [[ -z "${HACKME_CANONICAL_RELAY_ADMIN_TOKEN:-}" && -n "${NODE_SSH:-}" ]]; then
  relay="$(ssh -o BatchMode=yes -o ConnectTimeout=8 "${NODE_SSH}" "grep '^HACKME_ADMIN_TOKEN=' '${NODE_DEPLOY_DIR:-/opt/hackme}/.env.vps' 2>/dev/null | cut -d= -f2-" 2>/dev/null || true)"
  if [[ -n "$relay" ]]; then
    export HACKME_CANONICAL_RELAY_ADMIN_TOKEN="$relay"
    echo "[desktop-up] HACKME_CANONICAL_RELAY_ADMIN_TOKEN synced from VPS .env.vps"
  fi
fi
if [[ -n "${HACKME_CANONICAL_RELAY_ADMIN_TOKEN:-}" ]]; then
  if grep -q '^HACKME_CANONICAL_RELAY_ADMIN_TOKEN=' "$DESKTOP_ENV_FILE" 2>/dev/null; then
    sed -i "s|^HACKME_CANONICAL_RELAY_ADMIN_TOKEN=.*|HACKME_CANONICAL_RELAY_ADMIN_TOKEN=${HACKME_CANONICAL_RELAY_ADMIN_TOKEN}|" "$DESKTOP_ENV_FILE"
  else
    echo "HACKME_CANONICAL_RELAY_ADMIN_TOKEN=${HACKME_CANONICAL_RELAY_ADMIN_TOKEN}" >>"$DESKTOP_ENV_FILE"
  fi
fi

if [[ "${DESKTOP_PROFILE}" == "command" ]]; then
  export HACKME_CHAIN_LEADER_LOCAL_POH=1
  unset HACKME_POOL_COORDINATOR_URL HACKME_POOL_COORDINATOR_TOKEN 2>/dev/null || true
elif [[ "${DESKTOP_PROFILE}" == "worker" ]]; then
  unset HACKME_CHAIN_LEADER_LOCAL_POH 2>/dev/null || true
  # Do not force localhost:18081 when public authority infers coordinator from HTTPS.
  if [[ -z "${HACKME_POOL_COORDINATOR_URL:-}" && -z "${HACKME_PUBLIC_AUTHORITY_BASE:-}" ]]; then
    export HACKME_POOL_COORDINATOR_URL="${COORD_URL}"
  fi
else
  echo "[desktop-up] unknown DESKTOP_PROFILE=${DESKTOP_PROFILE} (use command|worker)" >&2
  exit 2
fi

# Public pool hybrid signer: resolveMinersignBinPath() checks Dir(os.Executable()) first for
# logs/desktop/hackme-node-desktop → minersign must live in LOG_DIR (not only repo root).
build_desktop_minersign() {
  echo "[desktop-up] go build minersign -> $LOG_DIR/minersign (+ $ROOT_DIR/minersign)"
  go build -trimpath -o "$LOG_DIR/minersign" ./cmd/minersign
  chmod 755 "$LOG_DIR/minersign"
  cp -f "$LOG_DIR/minersign" "$ROOT_DIR/minersign"
  chmod 755 "$ROOT_DIR/minersign"
}
build_desktop_minersign

ensure_from_code_toolchains
sync_toolchain_env_to_log_dir
source_toolchain_env

if [[ -f "$PID_FILE" ]]; then
  old_pid="$(cat "$PID_FILE" 2>/dev/null || true)"
  if [[ -n "$old_pid" ]] && kill -0 "$old_pid" >/dev/null 2>&1; then
    if [[ "${TOOLCHAINS_INSTALLED:-0}" == "1" ]]; then
      echo "[desktop-up] restarting node to load new from_code toolchains (pid=$old_pid)"
      kill "$old_pid" 2>/dev/null || true
      sleep 1
      rm -f "$PID_FILE"
    else
      echo "[desktop-up] node already running (pid=$old_pid)"
    fi
  else
    rm -f "$PID_FILE"
  fi
fi

desktop_gpu_build_tags() {
  if [[ "${HACKME_DESKTOP_GPU_BUILD:-1}" != "1" ]]; then
    return 0
  fi
  # NVIDIA: prefer native CUDA when toolkit is available.
  if command -v nvidia-smi >/dev/null 2>&1; then
    if [[ -f "$ROOT_DIR/scripts/ops/cuda_env.sh" ]]; then
      # shellcheck source=/dev/null
      if source "$ROOT_DIR/scripts/ops/cuda_env.sh" 2>/dev/null; then
        echo "cuda opencl"
        return 0
      fi
    fi
    if command -v nvcc >/dev/null 2>&1 || [[ -f /usr/local/cuda/include/nvrtc.h ]]; then
      echo "cuda opencl"
      return 0
    fi
  fi
  if pkg-config --exists OpenCL 2>/dev/null || [[ -f /usr/include/CL/cl.h ]] || [[ -f /usr/local/include/CL/cl.h ]]; then
    echo "opencl"
    return 0
  fi
}

if [[ ! -f "$PID_FILE" ]]; then
  gpu_tags="$(desktop_gpu_build_tags || true)"
  # desktop_gpu_build_tags may source cuda_env inside $(...) — re-apply for go build in this shell.
  if [[ -n "$gpu_tags" ]] && [[ "$gpu_tags" == *cuda* ]] && [[ -f "$ROOT_DIR/scripts/ops/cuda_env.sh" ]]; then
    # shellcheck source=/dev/null
    source "$ROOT_DIR/scripts/ops/cuda_env.sh" 2>/dev/null || true
  fi
  if [[ -n "$gpu_tags" ]]; then
    echo "[desktop-up] go build -tags ${gpu_tags} -> $NODE_BIN (GPU PoH enabled)"
    go build -trimpath -tags "$gpu_tags" -o "$NODE_BIN" .
  else
    echo "[desktop-up] go build -> $NODE_BIN (CPU PoH; install OpenCL/CUDA dev libs for GPU)"
    go build -trimpath -o "$NODE_BIN" .
  fi
  chmod 755 "$NODE_BIN"
  echo "[desktop-up] starting node on $BIND_ADDR (pid will be real server process, not go run)"
  # Inherit full environment from this shell (already sourced .env.desktop + profile exports)
  # so P2P, sync replay, CORS, and other HACKME_* vars reach the binary without listing each one.
  nohup "$NODE_BIN" >"$NODE_LOG_FILE" 2>&1 &
  echo "$!" >"$PID_FILE"
fi

for _ in $(seq 1 40); do
  if curl -fsS "$BASE_URL/api/status" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

if ! curl -fsS "$BASE_URL/api/status" >/dev/null 2>&1; then
  echo "[desktop-up] node did not become healthy, see $NODE_LOG_FILE" >&2
  exit 1
fi

open_app_window() {
  local url="$1"
  if command -v google-chrome >/dev/null 2>&1; then
    nohup google-chrome --app="$url" >/dev/null 2>&1 &
    return 0
  fi
  if command -v chromium-browser >/dev/null 2>&1; then
    nohup chromium-browser --app="$url" >/dev/null 2>&1 &
    return 0
  fi
  if command -v chromium >/dev/null 2>&1; then
    nohup chromium --app="$url" >/dev/null 2>&1 &
    return 0
  fi
  if command -v microsoft-edge >/dev/null 2>&1; then
    nohup microsoft-edge --app="$url" >/dev/null 2>&1 &
    return 0
  fi
  if command -v xdg-open >/dev/null 2>&1; then
    nohup xdg-open "$url" >/dev/null 2>&1 &
    return 0
  fi
  return 1
}

if open_app_window "$BASE_URL"; then
  echo "[desktop-up] opened dashboard window: $BASE_URL"
else
  echo "[desktop-up] node ready at $BASE_URL (open manually)"
fi

if [[ "${DESKTOP_PROFILE}" == "worker" && "${WORKER_AUTOSTART}" == "1" ]]; then
  coord_url_for_start="${HACKME_POOL_COORDINATOR_URL:-}"
  if [[ -z "$coord_url_for_start" ]]; then
    coord_url_for_start="$(curl -fsS "$BASE_URL/api/status" | python3 -c 'import sys,json; d=json.load(sys.stdin); print((d.get("pool_coordinator_url_effective") or d.get("pool_coordinator_url") or "").strip())' 2>/dev/null || true)"
  fi
  [[ -n "$coord_url_for_start" ]] || coord_url_for_start="$COORD_URL"
  export _HM_DESKTOP_COORD_URL="$coord_url_for_start"
  curl -fsS -X POST "$BASE_URL/api/worker/start" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
    -d "$(python3 -c 'import json,os; print(json.dumps({"coord_url": os.environ.get("_HM_DESKTOP_COORD_URL","")}))')" >/dev/null 2>&1 || true
  unset _HM_DESKTOP_COORD_URL
fi

echo "[desktop-up] pid_file=$PID_FILE log_file=$NODE_LOG_FILE env_file=$DESKTOP_ENV_FILE"
