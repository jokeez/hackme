#!/usr/bin/env bash
# After NVIDIA driver/library mismatch reboot: drop CPU fallback and restore CUDA miner.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DROP_IN="${HOME}/.config/systemd/user/hackme-kapa-worker.service.d/zz-cpu-fallback.conf"
ENV_DESKTOP="${ROOT}/.env.desktop"
LOG="${ROOT}/logs/post_reboot_gpu_restore.log"
mkdir -p "$(dirname "$LOG")"
exec >>"$LOG" 2>&1
echo "[gpu-restore $(date -u +%Y-%m-%dT%H:%M:%SZ)] start"

if ! nvidia-smi -L >/dev/null 2>&1; then
  echo "[gpu-restore] nvidia-smi still broken — leave CPU fallback in place"
  nvidia-smi -L 2>&1 | head -5 || true
  exit 1
fi
echo "[gpu-restore] nvidia-smi OK"
nvidia-smi -L || true

if [[ -f "$DROP_IN" ]]; then
  rm -f "$DROP_IN"
  echo "[gpu-restore] removed $DROP_IN"
fi

if [[ -f "$ENV_DESKTOP" ]]; then
  sed -i 's/^HACKME_GPU_BACKEND=cpu$/HACKME_GPU_BACKEND=cuda/' "$ENV_DESKTOP" || true
  sed -i 's/^GPU_BACKEND=cpu$/GPU_BACKEND=cuda/' "$ENV_DESKTOP" || true
  # keep WORKER_AUTOSTART=0 if systemd owns kapa
fi

systemctl --user daemon-reload
systemctl --user restart hackme-kapa-worker.service
sleep 3
systemctl --user is-active hackme-kapa-worker.service
pgrep -af 'workerpoh' | head -5 || true
echo "[gpu-restore] done"
