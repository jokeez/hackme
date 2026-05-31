#!/usr/bin/env bash
# One GPU, one worker_id, full batch — max GH/s and clean pool row (worker-kapa-pc).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

bash "$ROOT/scripts/ops/stop_local_pool_display_rig.sh" 2>/dev/null || true
pkill -f 'workerpoh.*worker-kapa-' 2>/dev/null || true
sleep 2

DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
[[ -f "$DESKTOP_ENV" ]] && set -a && . "$DESKTOP_ENV" && set +a

if [[ -n "${HACKME_ADMIN_TOKEN:-}" ]] && curl -fsS --max-time 2 "http://127.0.0.1:8080/api/status?lite=1" >/dev/null 2>&1; then
  curl -fsS -X POST "http://127.0.0.1:8080/api/worker/stop" \
    -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" >/dev/null 2>&1 || true
  sleep 1
  export COORD_TOKEN="${HACKME_POOL_COORDINATOR_TOKEN:-}"
  [[ -z "$COORD_TOKEN" && -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]] && \
    export COORD_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
  curl -fsS -X POST "http://127.0.0.1:8080/api/worker/start" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
    -d "$(python3 -c "import json,os; print(json.dumps({'coord_url':os.environ.get('HACKME_POOL_COORDINATOR_URL','https://hackme.tech/pool/coordinator'),'worker_id':'worker-kapa-pc','batch_size':int(os.environ.get('HACKME_WORKER_BATCH_SIZE','4194304')),'coord_token':os.environ.get('COORD_TOKEN',''),'gpu_backend':'cuda'}))")" \
    | python3 -m json.tool 2>/dev/null || true
  echo "[ideal-miner] started via node API (worker-kapa-pc)"
  exit 0
fi

# Fallback: direct CUDA worker
DATA_DIR="${HACKME_DATA_DIR:-$ROOT/data}"
SEED="$(tr -d '\r\n' <"$DATA_DIR/node_ed25519.seed")"
POOL_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_worker_token" 2>/dev/null || tr -d '\r\n' <"$ROOT/dist/release_0.1.0-rc11i/linux/pool.miner.token")"
mkdir -p "$ROOT/logs/ideal-miner"
HACKME_MINER_ED25519_SEED_HEX="$SEED" \
HACKME_WORKER_SIGN_SUBMITS=1 \
HACKME_DESKTOP_GPU_POOL=1 \
nohup "$ROOT/bin/workerpoh-cuda" \
  -coord "${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}" \
  -token "$POOL_TOKEN" \
  -worker worker-kapa-pc \
  -batch 4194304 \
  -gpu-chunk 4194304 \
  -search-timeout-ms 5000 \
  -gpu-backend cuda \
  >"$ROOT/logs/ideal-miner/worker-kapa-pc.log" 2>&1 &
echo "[ideal-miner] direct CUDA pid=$! log=logs/ideal-miner/worker-kapa-pc.log"
