#!/usr/bin/env bash
# Restart desktop node + pool worker (Linux). Picks opencl → cpu when NVIDIA driver is broken.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
LOG_DIR="${LOG_DIR:-$ROOT/logs/desktop}"
NODE_BIN="${NODE_BIN:-$LOG_DIR/hackme-node-desktop}"

set -a
# shellcheck disable=SC1090
[[ -f "$DESKTOP_ENV" ]] && . "$DESKTOP_ENV"
set +a

if [[ -z "${HACKME_ADMIN_TOKEN:-}" ]]; then
  echo "[restart-linux] HACKME_ADMIN_TOKEN missing in $DESKTOP_ENV" >&2
  exit 2
fi

export HACKME_DATA_DIR="${HACKME_DATA_DIR:-$ROOT/logs/desktop/data}"
mkdir -p "$LOG_DIR" "$HACKME_DATA_DIR" "$ROOT/bin" "$ROOT/logs"

load_coord_token() {
  if [[ -n "${HACKME_POOL_COORDINATOR_TOKEN:-}" ]]; then
    printf '%s' "$HACKME_POOL_COORDINATOR_TOKEN"
    return
  fi
  local wt="${ROOT}/.secrets/hackme_coordinator_worker_token"
  local at="${ROOT}/.secrets/hackme_coordinator_admin_token"
  if [[ -f "$wt" ]]; then
    tr -d '\r\n' <"$wt"
  elif [[ -f "$at" ]]; then
    tr -d '\r\n' <"$at"
  fi
}

echo "[restart-linux] build workers (cpu + opencl + cuda optional)"
go build -trimpath -o "$ROOT/bin/workerpoh-cpu" ./cmd/workerpoh
if pkg-config --exists OpenCL 2>/dev/null || [[ -f /usr/include/CL/cl.h ]]; then
  go build -trimpath -tags opencl -o "$ROOT/bin/workerpoh-opencl" ./cmd/workerpoh
fi
if bash "$ROOT/scripts/ops/detect_gpu_backend.sh" 2>/dev/null | grep -qx cuda; then
  bash "$ROOT/scripts/ops/build_cuda_worker.sh" 2>/dev/null || true
fi

pick_backend() {
  if [[ -n "${HACKME_GPU_BACKEND:-}" && "${HACKME_GPU_BACKEND}" != "auto" ]]; then
    echo "${HACKME_GPU_BACKEND}"
    return
  fi
  bash "$ROOT/scripts/ops/detect_gpu_backend.sh"
}

BACKEND="$(pick_backend)"
echo "[restart-linux] detected backend=$BACKEND"

# Stop stray workers
pkill -f 'scripts/ops/worker_autostart.sh' 2>/dev/null || true
pkill -f 'workerpoh-' 2>/dev/null || true
pkill -f 'bin/workerpoh' 2>/dev/null || true
sleep 2
rm -f "$ROOT/logs/.worker_autostart.lock"

# Node
if [[ ! -x "$NODE_BIN" ]]; then
  SKIP_TOOLCHAINS=1 HACKME_DESKTOP_GPU_BUILD=0 bash "$ROOT/scripts/ops/desktop_mode_up.sh" || {
    go build -trimpath -o "$NODE_BIN" .
  }
fi
if [[ -f "$LOG_DIR/node.pid" ]] && kill -0 "$(cat "$LOG_DIR/node.pid")" 2>/dev/null; then
  kill "$(cat "$LOG_DIR/node.pid")" 2>/dev/null || true
  sleep 1
fi
export HACKME_BIND_ADDR="${HACKME_BIND_ADDR:-127.0.0.1:8080}"
nohup "$NODE_BIN" >>"$LOG_DIR/node.log" 2>&1 &
echo $! >"$LOG_DIR/node.pid"
sleep 4
curl -fsS --max-time 8 -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
  "http://127.0.0.1:8080/api/status?lite=1" >/dev/null || {
  echo "[restart-linux] node not up — see $LOG_DIR/node.log" >&2
  exit 1
}

export HACKME_GPU_BACKEND="$BACKEND"
export HACKME_WORKER_SIGN_SUBMITS=1
export HACKME_DESKTOP_GPU_POOL=1
if [[ "$BACKEND" == "cpu" ]]; then
  export HACKME_GPU_DISABLE=1
  export WORKER_BIN="$ROOT/bin/workerpoh-cpu"
elif [[ "$BACKEND" == "opencl" ]]; then
  export HACKME_FORCE_OPENCL=1
  export WORKER_BIN="$ROOT/bin/workerpoh-opencl"
else
  export WORKER_BIN="$ROOT/bin/workerpoh-cuda"
fi

# Persist for node-managed worker subprocess
if grep -q '^HACKME_GPU_BACKEND=' "$DESKTOP_ENV" 2>/dev/null; then
  sed -i "s/^HACKME_GPU_BACKEND=.*/HACKME_GPU_BACKEND=$BACKEND/" "$DESKTOP_ENV"
else
  echo "HACKME_GPU_BACKEND=$BACKEND" >>"$DESKTOP_ENV"
fi

if [[ "$BACKEND" == "cpu" ]]; then
  COORD_TOKEN="$(load_coord_token)"
  SEED="$(tr -d '\r\n' <"$HACKME_DATA_DIR/node_ed25519.seed" 2>/dev/null || tr -d '\r\n' <"$HACKME_DATA_DIR/miner_submit_ed25519_seed.hex" 2>/dev/null || true)"
  export HACKME_MINER_ED25519_SEED_HEX="$SEED" HACKME_WORKER_SIGN_SUBMITS=1
  LOG="$ROOT/logs/workerpoh-worker-kapa-pc-$(date -u +%Y%m%dT%H%M%SZ).log"
  nohup "$ROOT/bin/workerpoh-cpu" -coord "${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}" \
    -token "$COORD_TOKEN" -worker "${WORKER_ID:-worker-kapa-pc}" -batch 524288 -gpu-disable >>"$LOG" 2>&1 &
  echo "[restart-linux] direct CPU worker pid=$! log=$LOG"
  sleep 8
  pgrep -af workerpoh-cpu && exit 0
fi
if [[ "$BACKEND" == "opencl" ]]; then
  bash "$ROOT/scripts/ops/desktop_worker_reset.sh" && exit 0
fi

bash "$ROOT/scripts/ops/desktop_worker_reset.sh" || {
  echo "[restart-linux] worker_reset failed; trying API start with backend=$BACKEND"
  COORD_TOKEN="$(load_coord_token)"
  curl -fsS -X POST -H "Content-Type: application/json" -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
    -d "{\"coord_url\":\"${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}\",\"worker_id\":\"${WORKER_ID:-worker-kapa-pc}\",\"batch_size\":${HACKME_WORKER_BATCH_SIZE:-524288},\"coord_token\":\"$COORD_TOKEN\",\"gpu_backend\":\"$BACKEND\"}" \
    "http://127.0.0.1:8080/api/worker/start"
}

sleep 6
pgrep -af 'workerpoh|hackme-node-desktop' | head -6 || true
echo "[restart-linux] done backend=$BACKEND"
