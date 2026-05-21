#!/usr/bin/env bash
# Bring Windows RX580 rig to ideal pool settings and verify pool GH/s.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WIN_SSH="${WIN_SSH:-hackme-windows}"
WIN_DIR='C:/HackMe'

echo "[win-ideal] sync opencl worker + scripts"
scp -q "$ROOT/dist/windows/workerpoh-opencl.exe" "$WIN_SSH:$WIN_DIR/workerpoh-opencl.exe" 2>/dev/null || {
  echo "[win-ideal] building opencl exe..."
  bash "$ROOT/scripts/release/windows/build_workerpoh_opencl.sh" "$ROOT/dist/windows/workerpoh-opencl.exe"
  scp -q "$ROOT/dist/windows/workerpoh-opencl.exe" "$WIN_SSH:$WIN_DIR/workerpoh-opencl.exe"
}
for f in write_hackme_env.ps1 detect_gpu.ps1 windows_fix_env_and_restart.ps1 autostart_pool_worker.bat; do
  src="$ROOT/scripts/release/windows/$f"
  [[ "$f" == "windows_fix_env_and_restart.ps1" ]] && src="$ROOT/scripts/ops/windows_fix_env_and_restart.ps1"
  scp -q "$src" "$WIN_SSH:$WIN_DIR/$f"
done

echo "[win-ideal] detect GPU + env + restart"
ssh -o BatchMode=yes "$WIN_SSH" "powershell -NoProfile -ExecutionPolicy Bypass -Command \"
  Set-Location '$WIN_DIR'
  & .\\detect_gpu.ps1 -OutFile .\\gpu_detect.json
  & .\\write_hackme_env.ps1 -InstallDir '$WIN_DIR' -GpuBackend opencl -RigProfile amd_rx580_2048sp -NonInteractive
  & .\\windows_fix_env_and_restart.ps1 '$WIN_DIR'
\""

echo "[win-ideal] wait for pool stats..."
sleep 35
ssh -o BatchMode=yes "${NODE_SSH:-hackme-vps}" 'TOK=$(tr -d "\r\n" </opt/hackme/.secrets/hackme_coordinator_admin_token); curl -fsS -H "X-Hackme-Admin-Token: $TOK" http://127.0.0.1:18081/api/work/stats' | python3 -c "
import json,sys
d=json.load(sys.stdin)
print('pool_gh_s', round(d.get('pool_hashrate_gh_s',0),3))
for r in sorted(d.get('active_rigs',[]), key=lambda x:-x.get('hashrate_gh_s',0)):
    print(' ', r.get('worker_id'), round(r.get('hashrate_gh_s',0),3))
"
