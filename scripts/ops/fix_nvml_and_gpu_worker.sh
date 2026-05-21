#!/usr/bin/env bash
# Fix NVIDIA NVML mismatch (580.x kernel vs userspace) and restart pool GPU worker.
# Requires sudo for module reload OR reboot if reload fails.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[nvml-fix] current state:"
nvidia-smi -L 2>&1 || true
echo "kernel: $(cat /proc/driver/nvidia/version 2>/dev/null | head -1 || echo '?')"

if nvidia-smi -L >/dev/null 2>&1; then
  echo "[nvml-fix] NVML OK — applying desktop GPU pool"
  bash "$ROOT/scripts/ops/apply_desktop_gpu_pool.sh"
  bash "$ROOT/scripts/ops/desktop_worker_reset.sh"
  exit 0
fi

echo "[nvml-fix] NVML broken — try module reload (sudo)..."
if sudo modprobe -r nvidia_uvm nvidia_drm nvidia_modeset nvidia 2>/dev/null; then
  sudo modprobe nvidia
  sleep 2
fi

if nvidia-smi -L >/dev/null 2>&1; then
  echo "[nvml-fix] NVML fixed after reload"
  bash "$ROOT/scripts/ops/apply_desktop_gpu_pool.sh"
  bash "$ROOT/scripts/ops/desktop_worker_reset.sh"
  exit 0
fi

echo "[nvml-fix] still broken — REBOOT required, then run:"
echo "  bash scripts/ops/apply_desktop_gpu_pool.sh"
echo "  bash scripts/ops/desktop_worker_reset.sh"
exit 1
