#!/usr/bin/env bash
# Stop duplicate pool workers on desktop and start a single GPU worker.
# When HACKME_GPU_BACKEND=cuda, starts workerpoh-cuda directly (fresh .env.desktop env).
# Otherwise uses local node API (may inherit stale GPU backend until node restart).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

if [[ "${FORCE_MINING:-0}" != "1" ]]; then
  for pause in "$ROOT_DIR/logs/desktop/mining_paused" "$ROOT_DIR/logs/mining_paused"; do
    if [[ -f "$pause" ]]; then
      echo "[worker-reset] mining paused ($pause) — use resume_pool_mining.sh or FORCE_MINING=1" >&2
      exit 0
    fi
  done
fi

DESKTOP_ENV_FILE="${DESKTOP_ENV_FILE:-$ROOT_DIR/.env.desktop}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
SECRET_COORD="${SECRET_COORD:-$ROOT_DIR/.secrets/hackme_coordinator_admin_token}"
WORKER_TOKEN_FILE="${WORKER_TOKEN_FILE:-$ROOT_DIR/.secrets/hackme_coordinator_worker_token}"

set -a
# shellcheck disable=SC1090
[[ -f "$DESKTOP_ENV_FILE" ]] && . "$DESKTOP_ENV_FILE"
set +a

if [[ -z "${HACKME_ADMIN_TOKEN:-}" ]]; then
  echo "[worker-reset] HACKME_ADMIN_TOKEN missing in $DESKTOP_ENV_FILE" >&2
  exit 2
fi
# Pool claim/submit: prefer scoped worker token (not admin) when available.
if [[ -f "$WORKER_TOKEN_FILE" ]]; then
  export HACKME_POOL_COORDINATOR_TOKEN="$(tr -d '\r\n' <"$WORKER_TOKEN_FILE")"
elif [[ -z "${HACKME_POOL_COORDINATOR_TOKEN:-}" && -f "$SECRET_COORD" ]]; then
  export HACKME_POOL_COORDINATOR_TOKEN="$(tr -d '\r\n' <"$SECRET_COORD")"
fi
if [[ -z "${HACKME_POOL_COORDINATOR_TOKEN:-}" ]]; then
  echo "[worker-reset] create $WORKER_TOKEN_FILE or set HACKME_POOL_COORDINATOR_TOKEN" >&2
  exit 2
fi

COORD_URL="${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}"
WORKER_ID="${WORKER_ID:-worker-kapa-pc}"
BATCH_SIZE="${HACKME_WORKER_BATCH_SIZE:-16777216}"
GPU_BACKEND="${HACKME_GPU_BACKEND:-auto}"

if [[ "$GPU_BACKEND" == "auto" || -z "$GPU_BACKEND" ]]; then
  GPU_BACKEND="$(HACKME_REPO_ROOT="$ROOT_DIR" bash "$ROOT_DIR/scripts/ops/detect_gpu_backend.sh" 2>/dev/null || echo cpu)"
fi
if [[ "$GPU_BACKEND" == "cuda" ]] && ! nvidia-smi -L >/dev/null 2>&1; then
  echo "[worker-reset] WARN: cuda requested but NVIDIA driver unhealthy — re-detecting backend" >&2
  GPU_BACKEND="$(HACKME_GPU_BACKEND=auto HACKME_REPO_ROOT="$ROOT_DIR" bash "$ROOT_DIR/scripts/ops/detect_gpu_backend.sh" 2>/dev/null || echo opencl)"
  export HACKME_GPU_BACKEND="$GPU_BACKEND"
fi
if [[ "$GPU_BACKEND" == "opencl" && ! -x "$ROOT_DIR/bin/workerpoh-opencl" ]]; then
  echo "[worker-reset] building bin/workerpoh-opencl..."
  (cd "$ROOT_DIR" && go build -trimpath -tags opencl -o "$ROOT_DIR/bin/workerpoh-opencl" ./cmd/workerpoh)
fi

load_miner_seed_hex() {
  local data_dir="${HACKME_DATA_DIR:-$ROOT_DIR/logs/desktop/data}"
  local seed=""
  if [[ -f "$data_dir/node_ed25519.seed" ]]; then
    seed="$(tr -d '\r\n' <"$data_dir/node_ed25519.seed")"
  elif [[ -f "$data_dir/miner_submit_ed25519_seed.hex" ]]; then
    seed="$(tr -d '\r\n' <"$data_dir/miner_submit_ed25519_seed.hex")"
  elif [[ -f "$ROOT_DIR/data/miner_submit_ed25519_seed.hex" ]]; then
    seed="$(tr -d '\r\n' <"$ROOT_DIR/data/miner_submit_ed25519_seed.hex")"
  fi
  seed="${seed,,}"
  if [[ ${#seed} -ne 64 ]]; then
    echo "[worker-reset] miner seed missing (need 64 hex in $data_dir)" >&2
    return 1
  fi
  export HACKME_MINER_ED25519_SEED_HEX="$seed"
}

stop_all_workers() {
  echo "[worker-reset] stopping node-managed worker..."
  curl -fsS -X POST "$BASE_URL/api/worker/stop" \
    -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" >/dev/null 2>&1 || true

  echo "[worker-reset] killing stray worker_loop / workerpoh processes..."
  pkill -f 'scripts/ops/worker_loop.sh' 2>/dev/null || true
  pkill -f 'scripts/ops/worker_autostart.sh' 2>/dev/null || true
  pkill -f 'workerpoh-opencl' 2>/dev/null || true
  pkill -f 'workerpoh-cuda' 2>/dev/null || true
  pkill -f 'workerpoh-cpu' 2>/dev/null || true
  pkill -f 'workerpoh ' 2>/dev/null || true
  sleep 2

  echo "[worker-reset] clearing stale pool worker logs..."
  : >"$ROOT_DIR/logs/worker_participant.log" 2>/dev/null || true
}

start_cuda_worker_direct() {
  local cuda_bin="$ROOT_DIR/bin/workerpoh-cuda"
  if [[ ! -x "$cuda_bin" ]]; then
    echo "[worker-reset] $cuda_bin missing; run: bash scripts/ops/build_cuda_worker.sh" >&2
    return 1
  fi
  load_miner_seed_hex

  if [[ -f "$ROOT_DIR/scripts/ops/cuda_env.sh" ]]; then
    set +u
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/ops/cuda_env.sh" 2>/dev/null || true
    set -u
  fi

  export COORD_URL="$COORD_URL"
  export COORD_TOKEN="${HACKME_POOL_COORDINATOR_TOKEN}"
  export COORD_ADMIN_TOKEN="${HACKME_POOL_COORDINATOR_TOKEN}"
  export WORKER_ID
  export BATCH_SIZE
  export GPU_CHUNK="${GPU_CHUNK:-$BATCH_SIZE}"
  export SEARCH_TIMEOUT_MS="${SEARCH_TIMEOUT_MS:-12000}"
  export HACKME_GPU_BACKEND=cuda
  export WORKER_BIN="$cuda_bin"
  export HACKME_DESKTOP_GPU_POOL=1
  export SKIP_WORKER_BUILD=1
  if [[ -n "${HACKME_WORKER_HASHRATE_GHS:-}" ]]; then
    export HASHRATE_GHS="${HACKME_WORKER_HASHRATE_GHS}"
  else
    unset HASHRATE_GHS 2>/dev/null || true
  fi
  export HACKME_WORKER_CLAIM_TIMEOUT="${HACKME_WORKER_CLAIM_TIMEOUT:-90s}"
  export HACKME_WORKER_SUBMIT_TIMEOUT="${HACKME_WORKER_SUBMIT_TIMEOUT:-120s}"
  export HACKME_WORKER_CLAIM_COOLDOWN_MS="${HACKME_WORKER_CLAIM_COOLDOWN_MS:-100}"
  export HACKME_WORKER_SIGN_SUBMITS=1
  local safe_wid="${WORKER_ID//[^a-zA-Z0-9_-]/_}"
  export HACKME_MINER_NONCE_FILE="$ROOT_DIR/logs/miner_submit_nonce.${safe_wid}.seq"

  echo "[worker-reset] CUDA direct start: bin=$cuda_bin worker=$WORKER_ID batch=$BATCH_SIZE"
  if [[ -x "$ROOT_DIR/scripts/ops/build_cuda_worker.sh" ]]; then
    bash "$ROOT_DIR/scripts/ops/build_cuda_worker.sh"
    cuda_bin="$ROOT_DIR/bin/workerpoh-cuda"
  fi
  nohup bash "$ROOT_DIR/scripts/ops/worker_autostart.sh" >>"$ROOT_DIR/logs/worker_participant.log" 2>&1 &
  local apid=$!
  echo "[worker-reset] worker_autostart pid=$apid (log=logs/worker_participant.log)"
  sleep 4
  if pgrep -af workerpoh-cuda >/dev/null 2>&1; then
    echo "[worker-reset] OK: workerpoh-cuda running"
    pgrep -af workerpoh-cuda || true
    local latest
    latest="$(ls -t "$ROOT_DIR"/logs/workerpoh-"${WORKER_ID}"-*.log 2>/dev/null | head -1 || true)"
    if [[ -n "$latest" ]]; then
      echo "[worker-reset] latest worker log ($latest):"
      tail -n 5 "$latest" || true
    fi
    return 0
  fi
  echo "[worker-reset] WARN: workerpoh-cuda not running; tail worker_participant.log:" >&2
  tail -n 30 "$ROOT_DIR/logs/worker_participant.log" >&2 || true
  return 1
}

stop_all_workers

nvidia_driver_ok() {
  command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi -L >/dev/null 2>&1
}

if [[ "$GPU_BACKEND" == "cuda" ]]; then
  if nvidia_driver_ok; then
    start_cuda_worker_direct && exit $?
  fi
  echo "[worker-reset] WARN: NVIDIA driver unhealthy (NVML mismatch?) — falling back from cuda" >&2
  GPU_BACKEND="$(HACKME_REPO_ROOT="$ROOT_DIR" bash "$ROOT_DIR/scripts/ops/detect_gpu_backend.sh" 2>/dev/null || echo opencl)"
  export HACKME_GPU_BACKEND="$GPU_BACKEND"
  if [[ "$GPU_BACKEND" == "opencl" ]]; then
    export HACKME_FORCE_OPENCL=1
  fi
  if [[ "$GPU_BACKEND" == "cpu" ]]; then
    export HACKME_GPU_DISABLE=1
  fi
fi

if [[ "$GPU_BACKEND" == "opencl" && -x "$ROOT_DIR/bin/workerpoh-opencl" ]]; then
  load_miner_seed_hex
  export COORD_URL="$COORD_URL"
  export COORD_TOKEN="${HACKME_POOL_COORDINATOR_TOKEN}"
  export WORKER_ID BATCH_SIZE
  export WORKER_BIN="$ROOT_DIR/bin/workerpoh-opencl"
  export HACKME_GPU_BACKEND=opencl
  export HACKME_FORCE_OPENCL=1
  export HACKME_DESKTOP_GPU_POOL=1
  export SKIP_WORKER_BUILD=1
  export HACKME_WORKER_SIGN_SUBMITS=1
  safe_wid="${WORKER_ID//[^a-zA-Z0-9_-]/_}"
  export HACKME_MINER_NONCE_FILE="$ROOT_DIR/logs/miner_submit_nonce.${safe_wid}.seq"
  nohup bash "$ROOT_DIR/scripts/ops/worker_autostart.sh" >>"$ROOT_DIR/logs/worker_participant.log" 2>&1 &
  echo "[worker-reset] opencl autostart pid=$! (bin/workerpoh-opencl)"
  sleep 6
  pgrep -af workerpoh-opencl && exit 0
  echo "[worker-reset] opencl failed — see logs/worker_participant.log" >&2
  tail -n 20 "$ROOT_DIR/logs/worker_participant.log" >&2 || true
fi

echo "[worker-reset] starting single worker id=$WORKER_ID coord=$COORD_URL (batch=$BATCH_SIZE backend=$GPU_BACKEND)"
start_json="$(python3 - "$COORD_URL" "$WORKER_ID" "${HACKME_POOL_COORDINATOR_TOKEN:-}" "$BATCH_SIZE" "$GPU_BACKEND" <<'PY'
import json, sys
coord, wid, tok, batch, backend = sys.argv[1:6]
body = {"coord_url": coord, "worker_id": wid, "batch_size": int(batch or 4194304)}
if tok.strip():
    body["coord_token"] = tok.strip()
if backend.strip() and backend.strip() != "auto":
    body["gpu_backend"] = backend.strip()
print(json.dumps(body))
PY
)"
resp="$(curl -fsS -X POST "$BASE_URL/api/worker/start" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
  -d "$start_json")"
echo "$resp" | jq . 2>/dev/null || echo "$resp"

echo "[worker-reset] worker status:"
curl -fsS "$BASE_URL/api/worker/status" | jq . 2>/dev/null || true
