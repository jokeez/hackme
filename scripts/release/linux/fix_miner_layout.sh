#!/usr/bin/env bash
# Repair linux/ release folder so the node finds worker_autostart + bin/workerpoh-*.
set -euo pipefail

INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$INSTALL_DIR"

mkdir -p bin scripts/ops logs

link_ops() {
  local name="$1"
  local src="${INSTALL_DIR}/${name}"
  local dst="${INSTALL_DIR}/scripts/ops/${name}"
  if [[ -f "$dst" || -L "$dst" ]]; then
    return 0
  fi
  if [[ -f "$src" ]]; then
    ln -sf "../../${name}" "$dst"
    return 0
  fi
  if [[ -f "${INSTALL_DIR}/scripts/ops/${name}.orig" ]]; then
    return 0
  fi
  echo "[fix] WARN: missing ${name}" >&2
}

for op in worker_autostart.sh detect_gpu_backend.sh worker_loop.sh desktop_worker_reset.sh purge_stale_pool_workers.sh; do
  link_ops "$op"
done

link_bin() {
  local name="$1"
  local src="${INSTALL_DIR}/${name}"
  local dst="${INSTALL_DIR}/bin/${name}"
  [[ -x "$src" ]] || return 0
  [[ -e "$dst" ]] || ln -sf "../${name}" "$dst"
}

for wp in workerpoh workerpoh-opencl workerpoh-cuda workerpoh-cpu; do
  link_bin "$wp"
done
link_bin fleetplan
link_bin minersign

chmod +x scripts/ops/*.sh 2>/dev/null || true
chmod +x worker_autostart.sh detect_gpu_backend.sh 2>/dev/null || true
chmod +x hackme workerpoh* minersign fleetplan 2>/dev/null || true

echo "[fix] miner layout OK: ${INSTALL_DIR}"
echo "[fix] scripts/ops:"
ls -la scripts/ops/ 2>/dev/null || true
echo "[fix] bin:"
ls -la bin/ 2>/dev/null || true
