#!/usr/bin/env bash
# Cosmetic multi-miner display on one GPU: N pool worker_ids, one payout wallet.
# All workers share the node ed25519 seed → same HMC address for order escrow + settlement map.
#
# Usage:
#   bash scripts/ops/start_local_pool_display_rig.sh        # 3 rigs (default)
#   bash scripts/ops/start_local_pool_display_rig.sh 4
#   WALLET=HMC-... bash scripts/ops/start_local_pool_display_rig.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

N="${1:-3}"
WALLET="${WALLET:-HMC-91fe007e4036c602}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
DATA_DIR="${HACKME_DATA_DIR:-$ROOT/data}"
LOG_DIR="$ROOT/logs/pool-display-rig"
WORKER_BIN="${WORKER_BIN:-$ROOT/bin/workerpoh-cuda}"
[[ -x "$WORKER_BIN" ]] || WORKER_BIN="$ROOT/bin/workerpoh"

POOL_TOKEN="${POOL_TOKEN:-}"
if [[ -z "$POOL_TOKEN" && -f "$ROOT/.secrets/hackme_coordinator_worker_token" ]]; then
  POOL_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_worker_token")"
fi
if [[ -z "$POOL_TOKEN" && -f "$ROOT/dist/release_0.1.0-rc11h/linux/pool.miner.token" ]]; then
  POOL_TOKEN="$(tr -d '\r\n' <"$ROOT/dist/release_0.1.0-rc11h/linux/pool.miner.token")"
fi
if [[ -z "$POOL_TOKEN" ]]; then
  echo "[display-rig] set POOL_TOKEN or create .secrets/hackme_coordinator_worker_token" >&2
  exit 1
fi

load_unified_seed() {
  local seed=""
  for d in "$DATA_DIR" "$ROOT/logs/desktop/data" "$ROOT/data"; do
    if [[ -f "$d/node_ed25519.seed" ]]; then
      seed="$(tr -d '\r\n' <"$d/node_ed25519.seed")"
      break
    fi
  done
  seed="${seed,,}"
  if [[ ${#seed} -ne 64 ]]; then
    echo "[display-rig] need 64-hex node_ed25519.seed in $DATA_DIR" >&2
    exit 1
  fi
  export HACKME_MINER_ED25519_SEED_HEX="$seed"
}

log() { echo "[display-rig] $*"; }

mkdir -p "$LOG_DIR"

# Stop dashboard-managed single worker + any old fair/rig processes.
if [[ -f "$DESKTOP_ENV" ]]; then
  set -a
  # shellcheck disable=SC1090
  . "$DESKTOP_ENV"
  set +a
fi
if [[ -n "${HACKME_ADMIN_TOKEN:-}" ]] && curl -fsS --max-time 2 "http://127.0.0.1:8080/api/status?lite=1" >/dev/null 2>&1; then
  curl -fsS -X POST "http://127.0.0.1:8080/api/worker/stop" \
    -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" >/dev/null 2>&1 || true
fi
pkill -f 'workerpoh.*worker-kapa-' 2>/dev/null || true
sleep 2

load_unified_seed
ADDR="$(go run ./tools/show_node_addr "$DATA_DIR" 2>/dev/null || go run ./tools/show_node_addr "$ROOT/data")"
log "unified payout address: $ADDR (expect $WALLET)"

# Per-worker batch: share one GPU across N cosmetic ids (pool UI shows N miners).
BATCH_PER=$((4194304 / N))
[[ "$BATCH_PER" -ge 524288 ]] || BATCH_PER=524288

payout_entries="worker-kapa-pc=${WALLET}"
for i in $(seq 1 "$N"); do
  payout_entries+=",worker-kapa-rig-${i}=${WALLET}"
done
# Legacy ids (old runs) still settle to your wallet.
payout_entries+=",worker-kapa-fair-1=${WALLET},worker-kapa-fair-2=${WALLET},worker-kapa-fair-3=${WALLET}"
payout_entries+=",worker-vps-msk-01=${WALLET},worker-vps-62-01=${WALLET},vps-canary-01=${WALLET}"

if [[ -f "$DESKTOP_ENV" ]]; then
  if grep -q '^WORKER_PAYOUT_MAP=' "$DESKTOP_ENV"; then
    sed -i "s|^WORKER_PAYOUT_MAP=.*|WORKER_PAYOUT_MAP=${payout_entries}|" "$DESKTOP_ENV"
  else
    echo "WORKER_PAYOUT_MAP=${payout_entries}" >>"$DESKTOP_ENV"
  fi
  # Node watchdog would respawn worker-kapa-pc and fight the cosmetic rig fleet.
  if grep -q '^HACKME_WORKER_WATCHDOG=' "$DESKTOP_ENV"; then
    sed -i 's/^HACKME_WORKER_WATCHDOG=.*/HACKME_WORKER_WATCHDOG=0/' "$DESKTOP_ENV"
  else
    echo 'HACKME_WORKER_WATCHDOG=0' >>"$DESKTOP_ENV"
  fi
  log "updated $DESKTOP_ENV (payout map + WORKER_WATCHDOG=0)"
  log "restart node once if watchdog keeps respawning worker-kapa-pc: bash scripts/ops/desktop_mode_up.sh"
fi

log "starting $N cosmetic workers (batch=${BATCH_PER} each, same seed)"
for i in $(seq 1 "$N"); do
  wid="worker-kapa-rig-${i}"
  safe="${wid//[^a-zA-Z0-9_-]/_}"
  export HACKME_MINER_NONCE_FILE="$ROOT/logs/miner_submit_nonce.${safe}.seq"
  HACKME_MINER_ED25519_SEED_HEX="$HACKME_MINER_ED25519_SEED_HEX" \
  HACKME_WORKER_SIGN_SUBMITS=1 \
  HACKME_DESKTOP_GPU_POOL=1 \
  HACKME_GPU_FLEET=0 \
  nohup "$WORKER_BIN" \
    -coord "$COORD_URL" \
    -token "$POOL_TOKEN" \
    -worker "$wid" \
    -batch "$BATCH_PER" \
    -gpu-chunk "$BATCH_PER" \
    -search-timeout-ms 5000 \
    -gpu-backend cuda \
    >"$LOG_DIR/${wid}.log" 2>&1 &
  log "  $wid -> $ADDR pid=$!"
done

sleep 3
running="$(pgrep -cf 'workerpoh.*worker-kapa-rig-' || echo 0)"
log "running rig workers: $running / $N"
log "logs: $LOG_DIR/worker-kapa-rig-*.log"
log "stop: pkill -f 'workerpoh.*worker-kapa-rig-'"
