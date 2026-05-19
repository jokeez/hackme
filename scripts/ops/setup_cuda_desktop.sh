#!/usr/bin/env bash
# One-shot: install CUDA dev (sudo), build workerpoh-cuda, apply desktop pool.
#   bash ~/Desktop/HackMe/scripts/ops/setup_cuda_desktop.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
echo "[setup-cuda] repo=$ROOT"

if [[ ! -f /usr/include/nvrtc.h ]]; then
  echo "[setup-cuda] installing CUDA dev packages (sudo — may ask password)..."
  if ! sudo bash "$ROOT/scripts/ops/install_cuda_dev_ubuntu.sh"; then
    echo "[setup-cuda] install failed. Manual fix for broken v2raya apt repo:" >&2
    echo "  sudo mv /etc/apt/sources.list.d/v2raya.list /etc/apt/sources.list.d/v2raya.list.bak" >&2
    echo "  sudo apt-get update && sudo apt-get install -y libnvrtc-dev libnvrtc12 libcuda1" >&2
    exit 1
  fi
else
  echo "[setup-cuda] nvrtc.h already at /usr/include/nvrtc.h"
fi

bash "$ROOT/scripts/ops/build_cuda_worker.sh" --probe
bash "$ROOT/scripts/ops/apply_desktop_gpu_pool.sh"

LOG="$(ls -t "$ROOT"/logs/workerpoh-worker-kapa-pc-*.log 2>/dev/null | head -1)"
if [[ -n "$LOG" ]]; then
  echo ""
  echo "[setup-cuda] last worker log lines ($LOG):"
  tail -n 5 "$LOG" || true
fi
echo ""
echo "[setup-cuda] OK if log shows: [CUDA compute_120]  |  fallback: [OpenCL]"
