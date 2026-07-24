#!/usr/bin/env bash
# HackMe OS pool worker launcher (no Go toolchain on rig).
set -uo pipefail

ROOT="${HACKME_ROOT:-/opt/hackme}"
ENV_MAIN="/etc/hackme/miner.env"
ENV_STATE="/var/lib/hackme/miner.env"
RIG_ENV="/var/lib/hackme/rig.env"
POOL_TOKEN_FILE="/etc/hackme/pool.token"

if [[ -f "$ENV_MAIN" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_MAIN"
  set +a
fi
if [[ -f "$ENV_STATE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_STATE"
  set +a
fi
if [[ -f "$RIG_ENV" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$RIG_ENV"
  set +a
fi

COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
if [[ -z "${COORD_TOKEN:-}" && -f "$POOL_TOKEN_FILE" ]]; then
  COORD_TOKEN="$(tr -d '\r\n' <"$POOL_TOKEN_FILE")"
fi
export COORD_URL COORD_TOKEN
export HACKME_REPO_ROOT="$ROOT"
export SKIP_WORKER_BUILD=1
export HACKME_GPU_BACKEND="${HACKME_GPU_BACKEND:-auto}"

WORKER_ID="${WORKER_ID:-worker-$(hostname -s 2>/dev/null || echo rig)}"
export WORKER_ID

if [[ -z "${COORD_TOKEN:-}" || "${COORD_TOKEN}" == REPLACE_* ]]; then
  echo "[hackme-os] WARN: pool token missing — worker idle (rebuild ISO with pool token)" >&2
  sleep 3600
  exit 0
fi
if [[ -z "${HACKME_MINER_ED25519_SEED_HEX:-}" ]]; then
  echo "[hackme-os] WARN: miner seed missing — waiting for firstboot" >&2
  sleep 30
  exit 0
fi

detect="${ROOT}/scripts/ops/detect_gpu_backend.sh"
backend=cpu
if [[ -x "$detect" ]]; then
  backend="$("$detect")"
fi

choose_bin() {
  local b="$1"
  if [[ "$b" == "cuda" && -x "${ROOT}/bin/workerpoh-cuda" ]]; then
    echo "${ROOT}/bin/workerpoh-cuda"
  elif [[ "$b" == "opencl" && -x "${ROOT}/bin/workerpoh-opencl" ]]; then
    echo "${ROOT}/bin/workerpoh-opencl"
  elif [[ -x "${ROOT}/bin/workerpoh" ]]; then
    echo "${ROOT}/bin/workerpoh"
  else
    echo "${ROOT}/bin/workerpoh-cpu"
  fi
}

BIN="$(choose_bin "$backend")"
if [[ ! -x "$BIN" ]]; then
  echo "[hackme-os] ERROR: worker binary not found under ${ROOT}/bin (backend=${backend})" >&2
  exit 1
fi

BATCH_SIZE="${BATCH_SIZE:-${HACKME_WORKER_BATCH_SIZE:-16777216}}"
GPU_CHUNK="${GPU_CHUNK:-4194304}"
SEARCH_TIMEOUT_MS="${SEARCH_TIMEOUT_MS:-12000}"
export HACKME_WORKER_CLAIM_TIMEOUT="${HACKME_WORKER_CLAIM_TIMEOUT:-90s}"
export HACKME_WORKER_SUBMIT_TIMEOUT="${HACKME_WORKER_SUBMIT_TIMEOUT:-120s}"
export HACKME_DESKTOP_GPU_POOL="${HACKME_DESKTOP_GPU_POOL:-1}"
export HACKME_WORKER_CLAIM_COOLDOWN_MS="${HACKME_WORKER_CLAIM_COOLDOWN_MS:-100}"
# Optional: HACKME_POOL_DIRECT=1 or CF COORD_URL → worker_autostart prefers :18083

CPU_LIST=""
[[ -f /run/hackme-os/worker-cpu-list ]] && CPU_LIST="$(tr -d '\n' </run/hackme-os/worker-cpu-list)"

mkdir -p "${ROOT}/logs"
echo "[hackme-os] worker=${WORKER_ID} profile=${HACKME_RIG_PROFILE:-auto} backend=${backend} bin=$(basename "$BIN") cpus=${CPU_LIST:-all} coord=${COORD_URL}"

worker_args=(
  -coord "$COORD_URL"
  -token "$COORD_TOKEN"
  -worker "$WORKER_ID"
  -batch "$BATCH_SIZE"
  -gpu-chunk "$GPU_CHUNK"
  -search-timeout-ms "$SEARCH_TIMEOUT_MS"
  -gpu-backend "$backend"
)

if [[ -n "$CPU_LIST" ]] && command -v taskset >/dev/null 2>&1; then
  exec taskset -c "$CPU_LIST" "$BIN" "${worker_args[@]}"
fi
exec "$BIN" "${worker_args[@]}"
