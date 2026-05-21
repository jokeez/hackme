#!/usr/bin/env bash
# HackMe OS — GPU performance mode (AMD amdgpu sysfs, NVIDIA persistence).
set -euo pipefail

STATE_DIR="/run/hackme-os"
mkdir -p "$STATE_DIR"
LOG="${STATE_DIR}/gpu-tune.log"
exec >>"$LOG" 2>&1
echo "[hackme-os-gpu] $(date -u +%Y-%m-%dT%H:%M:%SZ)"

# --- AMD (Polaris / RDNA via amdgpu) ---
shopt -s nullglob
for card in /sys/class/drm/card*/device; do
  [[ -d "$card" ]] || continue
  echo "[hackme-os-gpu] AMD card: $card"
  if [[ -f "${card}/power_dpm_force_performance_level" ]]; then
    echo high >"${card}/power_dpm_force_performance_level" 2>/dev/null || \
      echo performance >"${card}/power_dpm_force_performance_level" 2>/dev/null || true
  fi
  if [[ -f "${card}/power_dpm_state" ]]; then
    echo performance >"${card}/power_dpm_state" 2>/dev/null || true
  fi
  # Compute profile (kernel 5.11+)
  if [[ -f "${card}/pp_power_profile_mode" ]]; then
    # mode 1 = compute on many Polaris/RDNA boards
    echo 1 >"${card}/pp_power_profile_mode" 2>/dev/null || true
  fi
  # Fan: auto
  if [[ -f "${card}/pwm1_enable" ]]; then
    echo 2 >"${card}/pwm1_enable" 2>/dev/null || true
  fi
  # Optional OC sysfs (when driver exposes pp_od_clk_voltage)
  if [[ -f "${card}/pp_od_clk_voltage" && "${HACKME_OS_GPU_OC:-1}" == "1" ]]; then
    echo "[hackme-os-gpu] pp_od_clk_voltage present — use hackme-os-gpu-oc for manual OC"
  fi
done

# --- NVIDIA ---
if command -v nvidia-smi >/dev/null 2>&1; then
  nvidia-smi -pm 1 2>/dev/null || true
  nvidia-smi --auto-boost-permission=0 2>/dev/null || true
  nvidia-smi --auto-boost-default=0 2>/dev/null || true
  # Prefer max clocks when power limit allows
  nvidia-smi -acp 0 2>/dev/null || true
  for i in $(nvidia-smi -L 2>/dev/null | sed -n 's/^GPU \([0-9]*\).*/\1/p'); do
    nvidia-smi -i "$i" -pl 100 2>/dev/null || true
  done
  echo "[hackme-os-gpu] nvidia persistence on"
fi

# OpenCL ICD sanity
if command -v clinfo >/dev/null 2>&1; then
  clinfo -l 2>/dev/null | head -20 >"${STATE_DIR}/opencl-ls.txt" || true
fi
lspci 2>/dev/null | grep -iE 'vga|3d|display' >"${STATE_DIR}/pci-gpu.txt" || true

echo "[hackme-os-gpu] done"
