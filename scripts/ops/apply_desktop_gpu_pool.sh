#!/usr/bin/env bash
# Tune desktop (worker-kapa-pc) for max GPU pool throughput; throttle MSK CPU rig.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WALLET="${WALLET:-HMC-91fe007e4036c602}"
MSK_SSH="${MSK_SSH:-root@82.146.53.7}"

set_kv() {
  local f="$1" k="$2" v="$3"
  if grep -q "^${k}=" "$f" 2>/dev/null; then
    sed -i "s|^${k}=.*|${k}=${v}|" "$f"
  else
    echo "${k}=${v}" >>"$f"
  fi
}

echo "[desktop-gpu] patch .env.desktop"
DESKTOP_ENV="$ROOT/.env.desktop"
touch "$DESKTOP_ENV"
set_kv "$DESKTOP_ENV" HACKME_WORKER_BATCH_SIZE 4194304
set_kv "$DESKTOP_ENV" GPU_CHUNK 4194304
set_kv "$DESKTOP_ENV" SEARCH_TIMEOUT_MS 12000
set_kv "$DESKTOP_ENV" HACKME_WORKER_CLAIM_COOLDOWN_MS 0
set_kv "$DESKTOP_ENV" HACKME_GPU_BACKEND cuda
set_kv "$DESKTOP_ENV" HACKME_DESKTOP_GPU_POOL 1
set_kv "$DESKTOP_ENV" HACKME_WORKER_CLAIM_TIMEOUT 90s
set_kv "$DESKTOP_ENV" HACKME_WORKER_SUBMIT_TIMEOUT 120s
set_kv "$DESKTOP_ENV" WORKER_PAYOUT_MAP "worker-kapa-pc=${WALLET},worker-vps-msk-01=${WALLET},vps-canary-01=${WALLET}"

echo "[desktop-gpu] build GPU workerpoh (cuda if SDK present, else opencl)"
export GOCACHE="${GOCACHE:-$ROOT/.cache/go-build}"
mkdir -p "$ROOT/bin"
GPU_BACKEND=cuda
if command -v nvidia-smi >/dev/null 2>&1 && (cd "$ROOT" && go build -tags cuda -o "$ROOT/bin/workerpoh" ./cmd/workerpoh 2>/dev/null); then
  echo "[desktop-gpu] built bin/workerpoh with -tags cuda"
else
  GPU_BACKEND=opencl
  (cd "$ROOT" && go build -tags opencl -o "$ROOT/bin/workerpoh-opencl" ./cmd/workerpoh)
  echo "[desktop-gpu] built bin/workerpoh-opencl (install CUDA dev headers for native CUDA)"
fi
set_kv "$DESKTOP_ENV" HACKME_GPU_BACKEND "$GPU_BACKEND"

if ssh -o BatchMode=yes -o ConnectTimeout=8 "$MSK_SSH" true 2>/dev/null; then
  echo "[desktop-gpu] throttle MSK worker (CPU-only, slower claim loop)"
  COORD_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)"
  SEED="$(tr -d '\r\n' <"$ROOT/data/miner_submit_ed25519_seed.hex" 2>/dev/null || true)"
  ssh -o BatchMode=yes "$MSK_SSH" "bash -s" <<REMOTE
set -euo pipefail
DEPLOY=/opt/hackme-worker
ENVF="\$DEPLOY/.env.worker"
for kv in BATCH_SIZE=524288 HACKME_WORKER_CLAIM_COOLDOWN_MS=3000 HACKME_GPU_DISABLE=1; do
  k="\${kv%%=*}"; v="\${kv#*=}"
  if grep -q "^\${k}=" "\$ENVF" 2>/dev/null; then sed -i "s|^\${k}=.*|\${k}=\${v}|" "\$ENVF"; else echo "\${k}=\${v}" >>"\$ENVF"; fi
done
systemctl restart hackme-worker
REMOTE
fi

echo "[desktop-gpu] restart local worker"
if [[ -x "$ROOT/scripts/ops/desktop_worker_reset.sh" ]]; then
  bash "$ROOT/scripts/ops/desktop_worker_reset.sh" 2>&1 | tail -12
fi
echo "[desktop-gpu] done — stop/start pool worker from dashboard if reset skipped"
