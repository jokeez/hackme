#!/usr/bin/env bash
# Multi-GPU fleet (one Vast instance, N NVIDIA GPUs) — like real 6-GPU rig.
set -euo pipefail
PACK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-$PACK_ROOT/env.vast}"
REPORT="${REPORT:-$PACK_ROOT/reports/vast-session}"
LOG="$REPORT/fleet.log"
mkdir -p "$REPORT"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing $ENV_FILE" >&2
  exit 1
fi
set -a
# shellcheck disable=SC1091
source "$ENV_FILE"
set +a

: "${COORD_URL:?}"
: "${COORD_TOKEN:?}"
: "${HACKME_MINER_ED25519_SEED_HEX:?}"

WORKER_ID="${WORKER_ID:-vast-fleet-$(hostname -s)}"
RUN_SECONDS="${RUN_SECONDS:-2700}"
ROOT_DIR="$PACK_ROOT"

export COORD_URL COORD_TOKEN WORKER_ID
export HACKME_REPO_ROOT="$PACK_ROOT"
export HACKME_GPU_FLEET=1
export HACKME_GPU_FLEET_MAX="${HACKME_GPU_FLEET_MAX:-12}"
# Keep one worker row in UI while still using all GPUs.
export HACKME_GPU_FLEET_AGGREGATE_ID="${HACKME_GPU_FLEET_AGGREGATE_ID:-1}"
export HACKME_GPU_BACKEND=cuda
export HACKME_DESKTOP_GPU_POOL=1
export HACKME_WORKER_SIGN_SUBMITS=1
export SKIP_WORKER_BUILD=1
export BATCH_SIZE="${BATCH_SIZE:-4194304}"
export GPU_CHUNK="${GPU_CHUNK:-$BATCH_SIZE}"
export SEARCH_TIMEOUT_MS="${SEARCH_TIMEOUT_MS:-12000}"

AUTO="$PACK_ROOT/scripts/worker_autostart.sh"
if [[ ! -x "$AUTO" ]]; then
  echo "missing $AUTO — repack with scripts/ops/pack_vast_gpu_matrix.sh" >&2
  exit 1
fi

echo "[vast-fleet] base_worker=$WORKER_ID gpus=$(nvidia-smi -L 2>/dev/null | wc -l) run=${RUN_SECONDS}s"
echo "[vast-fleet] log=$LOG"

timeout --signal=INT "${RUN_SECONDS}" bash "$AUTO" 2>&1 | tee -a "$LOG" || true
echo "[vast-fleet] done — check coordinator for ${WORKER_ID}-gpu* workers"
