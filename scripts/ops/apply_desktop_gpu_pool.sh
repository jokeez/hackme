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
set_kv "$DESKTOP_ENV" HACKME_WORKER_HASHRATE_GHS 20
set_kv "$DESKTOP_ENV" GPU_CHUNK 4194304
set_kv "$DESKTOP_ENV" SEARCH_TIMEOUT_MS 12000
set_kv "$DESKTOP_ENV" HACKME_WORKER_CLAIM_COOLDOWN_MS 0
set_kv "$DESKTOP_ENV" HACKME_GPU_BACKEND cuda
set_kv "$DESKTOP_ENV" HACKME_DESKTOP_GPU_POOL 1
set_kv "$DESKTOP_ENV" HACKME_WORKER_CLAIM_TIMEOUT 90s
set_kv "$DESKTOP_ENV" HACKME_WORKER_SUBMIT_TIMEOUT 120s
set_kv "$DESKTOP_ENV" WORKER_PAYOUT_MAP "worker-kapa-pc=${WALLET},worker-vps-msk-01=${WALLET},vps-canary-01=${WALLET},worker-vps-62-01=${WALLET}"

echo "[desktop-gpu] build native CUDA workerpoh (production path)"
GPU_BACKEND=cpu
HAS_NVIDIA_SYSFS=0
if [[ -f /sys/class/drm/card1/device/vendor ]] && grep -q 0x10de /sys/class/drm/card1/device/vendor 2>/dev/null; then
  HAS_NVIDIA_SYSFS=1
fi
if command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi -L >/dev/null 2>&1; then
  if bash "$ROOT/scripts/ops/build_cuda_worker.sh" || [[ -x "$ROOT/bin/workerpoh-cuda" ]]; then
    GPU_BACKEND=cuda
    echo "[desktop-gpu] using bin/workerpoh-cuda (native CUDA)"
  fi
elif [[ "$HAS_NVIDIA_SYSFS" == "1" ]] && [[ -x "$ROOT/bin/workerpoh-cuda" ]]; then
  GPU_BACKEND=cuda
  echo "[desktop-gpu] WARN: nvidia-smi broken (NVML mismatch?) — trying CUDA binary anyway (reboot if worker fails)"
elif [[ "$HAS_NVIDIA_SYSFS" == "1" ]] || command -v clinfo >/dev/null 2>&1; then
  (cd "$ROOT" && go build -tags opencl -o "$ROOT/bin/workerpoh-opencl" ./cmd/workerpoh) 2>/dev/null || true
  if [[ -x "$ROOT/bin/workerpoh-opencl" ]]; then
    GPU_BACKEND=opencl
    ln -sf workerpoh-opencl "$ROOT/bin/workerpoh" 2>/dev/null || true
    echo "[desktop-gpu] using OpenCL (NVML/CUDA unavailable)"
  fi
fi
if [[ "$GPU_BACKEND" == "cpu" ]]; then
  echo "[desktop-gpu] WARN: GPU backend is cpu — fix NVML (reboot) or install OpenCL for ~0.01 GH/s only" >&2
fi
set_kv "$DESKTOP_ENV" HACKME_GPU_BACKEND "$GPU_BACKEND"
set_kv "$DESKTOP_ENV" HACKME_CUDA_VERBOSE 1

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
