#!/usr/bin/env bash
# Desktop: start PoH+fuzz inline hybrid (one workerpoh-cuda process).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

pkill -f "$ROOT/bin/workerfuzz" 2>/dev/null || true
pkill -f "$ROOT/bin/workerpoh-cuda" 2>/dev/null || true
sleep 1

set -a
# shellcheck disable=SC1091
source "$ROOT/.env.desktop"
set +a

export LD_LIBRARY_PATH="$ROOT/.deps/cuda-lib:${LD_LIBRARY_PATH:-}"
export HACKME_WORKER_HYBRID_FUZZ=1
export HACKME_WORKER_HYBRID_FUZZ_MODE=inline
export HACKME_GPU_BACKEND="${HACKME_GPU_BACKEND:-cuda}"

COORD="${HACKME_POOL_COORDINATOR_URL:?}"
# Pool fuzz claim currently accepts admin/worker token used by existing workerfuzz fleet.
TOK="${HACKME_COORDINATOR_ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-${HACKME_COORDINATOR_WORKER_TOKEN:?}}}"
WID="${WORKER_ID:-worker-kapa-pc}"
BATCH="${BATCH_SIZE:-16777216}"
LOG="logs/workerpoh-inline-hybrid-$(date -u +%Y%m%dT%H%M%SZ).log"
mkdir -p logs

# Prefer desktop node seed for hybrid payout (same HMC as PoH) when env seed unset.
if [[ -z "${HACKME_MINER_ED25519_SEED_HEX:-}" && -f logs/desktop/data/node_ed25519.seed ]]; then
  export HACKME_MINER_SEED_FILE="$ROOT/logs/desktop/data/node_ed25519.seed"
  # Also export hex so workerpoh hybrid_sign=true even before seed-file fallback lands in older bins.
  export HACKME_MINER_ED25519_SEED_HEX="$(tr -d ' \t\r\n' <"$ROOT/logs/desktop/data/node_ed25519.seed")"
fi

nohup "$ROOT/bin/workerpoh-cuda" \
  -coord "$COORD" \
  -token "$TOK" \
  -worker "$WID" \
  -batch "$BATCH" \
  -gpu-chunk "${GPU_CHUNK:-4194304}" \
  -gpu-backend cuda \
  -gpu-device 0 \
  >>"$LOG" 2>&1 &
PID=$!
echo "$PID" > logs/workerpoh-inline-hybrid.pid
echo "started pid=$PID log=$LOG"
sleep 6
if kill -0 "$PID" 2>/dev/null; then
  echo "alive=yes"
else
  echo "alive=no"
fi
grep -E 'hybrid fuzz|CUDA calibrated|FINDING|workerpoh-fuzz|panic|disabled|searcher=' "$LOG" | head -40 || true
