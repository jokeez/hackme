#!/usr/bin/env bash
# Run workerpoh-cuda against prod coordinator for RUN_SECONDS (default 30 min).
set -euo pipefail
PACK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-$PACK_ROOT/env.vast}"
REPORT="${REPORT:-$PACK_ROOT/reports/vast-session}"
LOG="$REPORT/worker.log"
mkdir -p "$REPORT"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing $ENV_FILE — copy env.vast.example and fill COORD_TOKEN + seed" >&2
  exit 1
fi
set -a
# shellcheck disable=SC1091
source "$ENV_FILE"
set +a

: "${COORD_URL:?set COORD_URL in env.vast}"
: "${COORD_TOKEN:?set COORD_TOKEN in env.vast}"
: "${HACKME_MINER_ED25519_SEED_HEX:?set HACKME_MINER_ED25519_SEED_HEX — run: ./bin/minersign -gen-seed}"

WORKER_ID="${WORKER_ID:-vast-$(hostname -s)-$$}"
RUN_SECONDS="${RUN_SECONDS:-1800}"
BIN="${WORKER_BIN:-$PACK_ROOT/bin/workerpoh-cuda}"
BATCH_SIZE="${BATCH_SIZE:-16777216}"

export COORD_URL COORD_TOKEN WORKER_ID
export HACKME_GPU_BACKEND="${HACKME_GPU_BACKEND:-cuda}"
export HACKME_WORKER_SIGN_SUBMITS="${HACKME_WORKER_SIGN_SUBMITS:-1}"
export GPU_CHUNK="${GPU_CHUNK:-$BATCH_SIZE}"

if [[ ! -x "$BIN" ]]; then
  echo "missing $BIN" >&2
  exit 1
fi

echo "[vast-worker] id=$WORKER_ID coord=$COORD_URL batch=$BATCH_SIZE run=${RUN_SECONDS}s"
echo "[vast-worker] log=$LOG"

timeout --signal=INT "${RUN_SECONDS}" "$BIN" \
  -coord "$COORD_URL" \
  -token "$COORD_TOKEN" \
  -worker "$WORKER_ID" \
  -batch "$BATCH_SIZE" \
  -gpu-chunk "${GPU_CHUNK}" \
  -search-timeout-ms "${SEARCH_TIMEOUT_MS:-12000}" \
  -gpu-backend cuda \
  2>&1 | tee -a "$LOG" || ec=$?
ec=${ec:-0}
echo "[vast-worker] exit=$ec (124=timeout OK for timed run)"
exit 0
