#!/usr/bin/env bash
# Run workerpoh-cuda against prod coordinator for RUN_SECONDS (default 30 min).
# Blake3 SHARE_STRICT: coordinator adaptClaimBatch sizes leases; local -batch is a floor.
set -euo pipefail
PACK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# When shipped inside dist/vast-blake3-pack, scripts live under pack/scripts and bins under pack/bin.
if [[ -x "$PACK_ROOT/bin/workerpoh-cuda" ]]; then
  :
elif [[ -x "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/bin/workerpoh-cuda" ]]; then
  PACK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi
ENV_FILE="${ENV_FILE:-$PACK_ROOT/env.vast}"
REPORT="${REPORT:-$PACK_ROOT/reports/vast-session}"
LOG="$REPORT/worker.log"
mkdir -p "$REPORT"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing $ENV_FILE — copy examples/env.vast.example → env.vast and fill tokens" >&2
  exit 1
fi
set -a
# shellcheck disable=SC1091
source "$ENV_FILE"
set +a

: "${COORD_URL:?set COORD_URL in env.vast}"
: "${COORD_TOKEN:?set COORD_TOKEN in env.vast}"
: "${HACKME_MINER_ED25519_SEED_HEX:?set HACKME_MINER_ED25519_SEED_HEX — ./bin/minersign -gen-seed}"

WORKER_ID="${WORKER_ID:-vast-$(hostname -s)-$$}"
RUN_SECONDS="${RUN_SECONDS:-1800}"
BIN="${WORKER_BIN:-$PACK_ROOT/bin/workerpoh-cuda}"
# 64M floor; coordinator grows toward ~lease_work_sec × credited GH/s (up to max_claim).
BATCH_SIZE="${BATCH_SIZE:-67108864}"
GPU_CHUNK="${GPU_CHUNK:-$BATCH_SIZE}"
SEARCH_TIMEOUT_MS="${SEARCH_TIMEOUT_MS:-8000}"

export COORD_URL COORD_TOKEN WORKER_ID
export HACKME_GPU_BACKEND="${HACKME_GPU_BACKEND:-cuda}"
export HACKME_WORKER_SIGN_SUBMITS="${HACKME_WORKER_SIGN_SUBMITS:-1}"
export HACKME_WORKER_HYBRID_FUZZ="${HACKME_WORKER_HYBRID_FUZZ:-0}"
export GPU_CHUNK

if [[ ! -x "$BIN" ]]; then
  echo "missing $BIN — run scripts/vast/build_blake3_pack.sh first" >&2
  exit 1
fi

echo "[vast-worker] id=$WORKER_ID coord=$COORD_URL batch=$BATCH_SIZE chunk=$GPU_CHUNK run=${RUN_SECONDS}s"
echo "[vast-worker] log=$LOG"
nvidia-smi -L 2>/dev/null || true

# A rented GPU box is shared: argv is world-readable through /proc, so the token goes
# in the environment (workerpoh defaults -token from COORD_TOKEN) and never on argv.
export COORD_TOKEN
timeout --signal=INT "${RUN_SECONDS}" "$BIN" \
  -coord "$COORD_URL" \
  -worker "$WORKER_ID" \
  -batch "$BATCH_SIZE" \
  -gpu-chunk "${GPU_CHUNK}" \
  -search-timeout-ms "${SEARCH_TIMEOUT_MS}" \
  -gpu-backend cuda \
  ${GPU_DEVICE:+-gpu-device "$GPU_DEVICE"} \
  2>&1 | tee -a "$LOG" || ec=$?
ec=${ec:-0}
echo "[vast-worker] exit=$ec (124=timeout OK for timed run)"
exit 0
