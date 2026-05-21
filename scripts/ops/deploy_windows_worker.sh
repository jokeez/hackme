#!/usr/bin/env bash
# Build workerpoh-opencl (Windows), sync to hackme-windows install, rewrite hackme.env, restart node+worker.
# Requires: ssh hackme-windows, Go, optional Docker for cross-compile.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
WIN_SSH="${WIN_SSH:-hackme-windows}"
WIN_DIR="${WIN_DIR:-/c/Program Files/HackMe}"
# Git Bash path on remote when using cmd over ssh
WIN_DIR_PS='C:\Program Files\HackMe'

echo "[deploy-win] build workerpoh-opencl.exe"
OUT="$ROOT/dist/windows/workerpoh-opencl.exe"
mkdir -p "$ROOT/dist/windows"
if command -v docker >/dev/null 2>&1 && [[ -f "$ROOT/scripts/release/windows/Dockerfile.opencl-mingw" ]]; then
  bash "$ROOT/scripts/release/windows/build_workerpoh_opencl.sh" "$OUT" 2>/dev/null || true
fi
if [[ ! -f "$OUT" ]]; then
  echo "[deploy-win] ERROR: no workerpoh-opencl.exe — build on Windows: pwsh -File scripts/release/windows/build_workerpoh_opencl.ps1" >&2
  exit 1
fi
if [[ -f "$ROOT/scripts/release/windows/build_workerpoh_opencl.ps1" ]]; then
  echo "[deploy-win] if OpenCL exe missing on host, build on Windows via build_workerpoh_opencl.ps1"
fi

echo "[deploy-win] scp scripts -> $WIN_SSH (do not overwrite opencl.exe unless OUT is a real OpenCL build)"
if [[ -f "$OUT" && "$(file -b "$OUT" 2>/dev/null)" != *"PE32+"* ]]; then
  echo "[deploy-win] skip binary upload — build OpenCL on the Windows host" >&2
else
  scp -q "$OUT" "$WIN_SSH:'C:/Program Files/HackMe/workerpoh-opencl.new.exe'" 2>/dev/null || true
fi
for f in write_hackme_env.ps1 detect_gpu.ps1 autostart_pool_worker.bat; do
  scp -q "$ROOT/scripts/release/windows/$f" "$WIN_SSH:$WIN_DIR/"
done

echo "[deploy-win] remote: detect GPU + rewrite hackme.env + restart"
ssh -o BatchMode=yes "$WIN_SSH" "powershell -NoProfile -ExecutionPolicy Bypass -Command \"
  Set-Location '$WIN_DIR_PS'
  if (Test-Path .\\detect_gpu.ps1) {
    & .\\detect_gpu.ps1 -OutFile .\\gpu_detect.json
  }
  & .\\write_hackme_env.ps1 -InstallDir '$WIN_DIR_PS' -GpuBackend opencl -RigProfile amd_rx580_generic -NonInteractive
  Get-Content hackme.env | Select-String 'HACKME_GPU_BACKEND|HACKME_BIND|HACKME_RIG|CALIBRATE|FLOOR'
  Stop-Process -Name hackme,workerpoh,workerpoh-opencl -Force -EA SilentlyContinue
  Start-Sleep 3
  Start-Process (Join-Path '$WIN_DIR_PS' 'hackme.exe') -WorkingDirectory '$WIN_DIR_PS' -WindowStyle Minimized
  Start-Sleep 15
  & .\\autostart_pool_worker.bat
  Start-Sleep 8
  Get-Process hackme,workerpoh* -EA SilentlyContinue | Format-Table Name,Id -AutoSize
\""

echo "[deploy-win] done"
