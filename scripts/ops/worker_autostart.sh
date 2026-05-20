#!/usr/bin/env bash
set -euo pipefail

# Universal worker launcher:
# - builds workerpoh if needed
# - auto-selects GPU backend/device for broad compatibility
# - runs in restart loop with bounded backoff
#
# Required env:
#   COORD_URL, COORD_TOKEN, HACKME_MINER_ED25519_SEED_HEX
# Optional env:
#   WORKER_ID, BATCH_SIZE, GPU_CHUNK, SEARCH_TIMEOUT_MS
#   HACKME_GPU_BACKEND=opencl|cuda|auto
#   HACKME_GPU_DEVICE=<int>
#   HACKME_GPU_DISABLE=1
#   WORKER_BIN=/path/to/workerpoh
#   RESTART_MAX_BACKOFF_SEC=20

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[worker-autostart] missing command: $1" >&2
    exit 1
  }
}

require_cmd go
require_cmd awk

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOG_DIR="${ROOT_DIR}/logs"
mkdir -p "$LOG_DIR"

LOCK_FILE="${LOG_DIR}/.worker_autostart.lock"
exec 200>"$LOCK_FILE"
if ! flock -n 200; then
  echo "[worker-autostart] another instance is already running (lock ${LOCK_FILE}); exiting"
  exit 0
fi

COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
COORD_TOKEN="${COORD_TOKEN:-${COORD_ADMIN_TOKEN:-${ADMIN_TOKEN:-}}}"
WORKER_ID="${WORKER_ID:-worker-$(hostname -s 2>/dev/null || echo local)}"
RESTART_MAX_BACKOFF_SEC="${RESTART_MAX_BACKOFF_SEC:-20}"

WORKER_ENV_FILE="${WORKER_ENV_FILE:-${ROOT_DIR}/.env.worker}"
if [[ -f "$WORKER_ENV_FILE" ]]; then
	set -a
	# shellcheck disable=SC1090
	source "$WORKER_ENV_FILE"
	set +a
fi

coord_looks_remote() {
  local u
  u="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
  [[ -z "$u" ]] && return 1
  [[ "$u" == *127.0.0.1* || "$u" == *localhost* || "$u" == *"::1"* ]] && return 1
  return 0
}

if coord_looks_remote "$COORD_URL"; then
  # Desktop GPU rigs: larger batches (fewer HTTPS round-trips, more attempts per range).
  if [[ -z "${BATCH_SIZE:-}" && "${HACKME_DESKTOP_GPU_POOL:-0}" == "1" ]]; then
    BATCH_SIZE=4194304
    GPU_CHUNK=4194304
  fi
  BATCH_SIZE="${BATCH_SIZE:-1048576}"
  GPU_CHUNK="${GPU_CHUNK:-$BATCH_SIZE}"
  export HACKME_WORKER_CLAIM_TIMEOUT="${HACKME_WORKER_CLAIM_TIMEOUT:-90s}"
  export HACKME_WORKER_SUBMIT_TIMEOUT="${HACKME_WORKER_SUBMIT_TIMEOUT:-120s}"
else
  BATCH_SIZE="${BATCH_SIZE:-4194304}"
  GPU_CHUNK="${GPU_CHUNK:-4194304}"
  export HACKME_WORKER_CLAIM_TIMEOUT="${HACKME_WORKER_CLAIM_TIMEOUT:-35s}"
  export HACKME_WORKER_SUBMIT_TIMEOUT="${HACKME_WORKER_SUBMIT_TIMEOUT:-90s}"
fi
if [[ "${HACKME_DESKTOP_GPU_POOL:-0}" == "1" ]]; then
  SEARCH_TIMEOUT_MS="${SEARCH_TIMEOUT_MS:-12000}"
  export HASHRATE_GHS="${HASHRATE_GHS:-${HACKME_WORKER_HASHRATE_GHS:-20}}"
else
  SEARCH_TIMEOUT_MS="${SEARCH_TIMEOUT_MS:-2500}"
fi
export HASHRATE_GHS="${HASHRATE_GHS:-${HACKME_WORKER_HASHRATE_GHS:-}}"

if [[ -z "${COORD_TOKEN}" ]]; then
  echo "[worker-autostart] set COORD_TOKEN (or COORD_ADMIN_TOKEN/ADMIN_TOKEN)" >&2
  exit 1
fi
if [[ -z "${HACKME_MINER_ED25519_SEED_HEX:-}" ]]; then
  echo "[worker-autostart] set HACKME_MINER_ED25519_SEED_HEX (64 hex chars)" >&2
  exit 1
fi

truthy() {
  local v
  v="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
  [[ "$v" == "1" || "$v" == "true" || "$v" == "yes" || "$v" == "on" ]]
}

detect_gpu_backend() {
  export HACKME_REPO_ROOT="${HACKME_REPO_ROOT:-$ROOT_DIR}"
  if [[ -x "${ROOT_DIR}/scripts/ops/detect_gpu_backend.sh" ]]; then
    bash "${ROOT_DIR}/scripts/ops/detect_gpu_backend.sh"
    return 0
  fi
  if truthy "${HACKME_GPU_DISABLE:-0}"; then
    echo "cpu"
    return 0
  fi
  if [[ -n "${HACKME_GPU_BACKEND:-}" && "${HACKME_GPU_BACKEND}" != "auto" ]]; then
    echo "${HACKME_GPU_BACKEND}"
    return 0
  fi
  echo "cpu"
}

choose_worker_bin() {
	local backend="${1:-cpu}"
	if [[ -n "${WORKER_BIN:-}" ]]; then
		printf '%s\n' "$WORKER_BIN"
		return 0
	fi
	if [[ "$backend" == "cpu" && -x "${ROOT_DIR}/bin/workerpoh" ]]; then
		printf '%s\n' "${ROOT_DIR}/bin/workerpoh"
		return 0
	fi
	if [[ "$backend" == "cuda" && -x "${ROOT_DIR}/bin/workerpoh-cuda" ]]; then
		printf '%s\n' "${ROOT_DIR}/bin/workerpoh-cuda"
		return 0
	fi
	if [[ "$backend" == "opencl" && -x "${ROOT_DIR}/bin/workerpoh-opencl" ]]; then
		printf '%s\n' "${ROOT_DIR}/bin/workerpoh-opencl"
		return 0
	fi
	if [[ -x "${ROOT_DIR}/bin/workerpoh-cuda" ]]; then
		printf '%s\n' "${ROOT_DIR}/bin/workerpoh-cuda"
		return 0
	fi
	if [[ -x "${ROOT_DIR}/bin/workerpoh-opencl" ]]; then
		printf '%s\n' "${ROOT_DIR}/bin/workerpoh-opencl"
		return 0
	fi
	if [[ -x "${ROOT_DIR}/bin/workerpoh" ]]; then
		printf '%s\n' "${ROOT_DIR}/bin/workerpoh"
		return 0
	fi
	printf '%s\n' "${ROOT_DIR}/bin/workerpoh"
}

build_worker_if_needed() {
	local bin="$1"
	local backend="$2"
	if [[ -x "$bin" ]] && truthy "${SKIP_WORKER_BUILD:-0}"; then
		return 0
	fi
	mkdir -p "$(dirname "$bin")"
	export GOCACHE="${GOCACHE:-${ROOT_DIR}/.cache/go-build}"
	mkdir -p "$GOCACHE" 2>/dev/null || true
	if [[ -x "$bin" ]] && ! truthy "${FORCE_WORKER_REBUILD:-0}"; then
		return 0
	fi
	if [[ -x "$bin" ]]; then
		local src="${ROOT_DIR}/cmd/workerpoh/main.go"
		if [[ -f "$src" && "$bin" -nt "$src" ]]; then
			return 0
		fi
		echo "[worker-autostart] rebuilding stale worker binary: ${bin}"
	fi
  echo "[worker-autostart] building worker binary: ${bin}"
  if [[ "$backend" == "cuda" ]]; then
    if [[ -x "$ROOT_DIR/scripts/ops/build_cuda_worker.sh" ]]; then
      OUT_CUDA="${ROOT_DIR}/bin/workerpoh-cuda"
      bash "$ROOT_DIR/scripts/ops/build_cuda_worker.sh"
      cp -f "$OUT_CUDA" "$bin" 2>/dev/null || ln -sf "$OUT_CUDA" "$bin"
    else
      (cd "$ROOT_DIR" && go build -tags "cuda,opencl" -o "$bin" ./cmd/workerpoh)
    fi
  elif [[ "$backend" == "opencl" ]]; then
    (cd "$ROOT_DIR" && go build -tags opencl -o "$bin" ./cmd/workerpoh)
  else
    (cd "$ROOT_DIR" && go build -o "$bin" ./cmd/workerpoh)
  fi
}

backend="$(detect_gpu_backend)"
# Pin native CUDA binary when configured (avoid stale opencl path after toolkit install).
if [[ "$backend" == "cuda" && -x "${ROOT_DIR}/bin/workerpoh-cuda" ]]; then
  export WORKER_BIN="${ROOT_DIR}/bin/workerpoh-cuda"
fi
bin_path="$(choose_worker_bin "$backend")"
build_worker_if_needed "$bin_path" "$backend"

bin_help="$("$bin_path" -h 2>&1 || true)"
supports_flag() {
  local flag="$1"
  [[ "$bin_help" == *"$flag"* ]]
}

gpu_backend_flag=()
gpu_device_flag=()
gpu_disable_flag=()

if [[ "$backend" == "cpu" ]]; then
  if supports_flag "-gpu-disable"; then
    gpu_disable_flag=(-gpu-disable)
  fi
else
  if supports_flag "-gpu-backend"; then
    gpu_backend_flag=(-gpu-backend "$backend")
  fi
fi
if [[ -n "${HACKME_GPU_DEVICE:-}" ]] && supports_flag "-gpu-device"; then
  gpu_device_flag=(-gpu-device "${HACKME_GPU_DEVICE}")
fi

count_gpus_for_backend() {
  local b="$1"
  case "$b" in
    cuda)
      if command -v nvidia-smi >/dev/null 2>&1; then
        nvidia-smi -L 2>/dev/null | grep -c -E '^GPU ' || echo 0
      else
        echo 0
      fi
      ;;
    opencl)
      if command -v clinfo >/dev/null 2>&1; then
        clinfo 2>/dev/null | awk '/Device Type/{if ($0 ~ /GPU/) n++} END{print n+0}'
        return
      fi
      local n=0 v
      for f in /sys/class/drm/card*/device/vendor; do
        [[ -f "$f" ]] || continue
        v="$(cat "$f" 2>/dev/null || true)"
        # AMD 0x1002, Intel 0x8086 (OpenCL fleet count when clinfo missing)
        if [[ "$v" == "0x1002" || "$v" == "4098" || "$v" == "0x8086" || "$v" == "32902" ]]; then
          n=$((n + 1))
        fi
      done
      echo "$n"
      ;;
    *) echo 0 ;;
  esac
}

worker_run_loop() {
  local worker_id="$1"
  local gpu_dev="${2:-}"
  local dev_flag=()
  if [[ -n "$gpu_dev" ]] && supports_flag "-gpu-device"; then
    dev_flag=(-gpu-device "$gpu_dev")
  fi
  local backoff=1
  while true; do
    ts="$(date +%Y%m%dT%H%M%S)"
    run_log="${LOG_DIR}/workerpoh-${worker_id}-${ts}.log"
    echo "[worker-autostart] launch worker=${worker_id} device=${gpu_dev:-auto} log=${run_log}"
    set +e
    "${bin_path}" \
      -coord "${COORD_URL}" \
      -token "${COORD_TOKEN}" \
      -worker "${worker_id}" \
      -batch "${BATCH_SIZE}" \
      -gpu-chunk "${GPU_CHUNK}" \
      -search-timeout-ms "${SEARCH_TIMEOUT_MS}" \
      "${gpu_backend_flag[@]}" \
      "${dev_flag[@]}" \
      "${gpu_disable_flag[@]}" \
      2>&1 | tee -a "${run_log}"
    rc="${PIPESTATUS[0]}"
    set -e
    echo "[worker-autostart] worker=${worker_id} exited rc=${rc}; restart in ${backoff}s"
    sleep "${backoff}"
    if (( backoff < RESTART_MAX_BACKOFF_SEC )); then
      backoff=$((backoff * 2))
      if (( backoff > RESTART_MAX_BACKOFF_SEC )); then
        backoff="${RESTART_MAX_BACKOFF_SEC}"
      fi
    fi
  done
}

echo "[worker-autostart] coord=${COORD_URL} worker=${WORKER_ID} backend=${backend} bin=${bin_path}"
echo "[worker-autostart] batch=${BATCH_SIZE} gpu_chunk=${GPU_CHUNK} timeout_ms=${SEARCH_TIMEOUT_MS}"

fleet_n=0
if [[ "$backend" != "cpu" ]] && [[ -z "${HACKME_GPU_DEVICE:-}" ]] && truthy "${HACKME_GPU_FLEET:-1}"; then
  fleet_n="$(count_gpus_for_backend "$backend")"
fi
if [[ "$fleet_n" =~ ^[0-9]+$ ]] && (( fleet_n > 1 )); then
  echo "[worker-autostart] multi-GPU fleet: ${fleet_n} devices (${backend})"
  fleet_pids=()
  for ((i = 0; i < fleet_n; i++)); do
    worker_run_loop "${WORKER_ID}-gpu${i}" "$i" &
    fleet_pids+=("$!")
  done
  wait "${fleet_pids[@]}"
else
  worker_run_loop "${WORKER_ID}" "${HACKME_GPU_DEVICE:-}"
fi

