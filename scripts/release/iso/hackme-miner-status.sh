#!/usr/bin/env bash
# Console-friendly pool status (ISO rigs without browser).
set -euo pipefail

ENV_STATE="/var/lib/hackme/miner.env"
[[ -f /etc/hackme/miner.env ]] && source /etc/hackme/miner.env
[[ -f "$ENV_STATE" ]] && source "$ENV_STATE"

COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
WORKER_ID="${WORKER_ID:-unknown}"

echo "=== HackMe OS $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
if [[ -f /run/hackme-os/topology.json ]]; then
  echo "cpus: $(cat /run/hackme-os/topology.json)"
fi
if [[ -f /var/lib/hackme/rig.env ]]; then
  grep -E '^HACKME_RIG_PROFILE=|^HACKME_GPU_BACKEND=|^BATCH_SIZE=' /var/lib/hackme/rig.env 2>/dev/null || true
fi
echo "worker: ${WORKER_ID}"
echo "pool:   ${COORD_URL}"

if command -v curl >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
  curl -fsS --max-time 8 "${COORD_URL}/api/work/stats" 2>/dev/null | jq -r '
    if .ok == false then "stats: unavailable"
    else
      "pool GH/s: " + ((.summary.pool_hashrate_gh_s // .pool_hashrate_gh_s // 0) | tostring),
      "workers:   " + ((.workers_count // (.workers | length) // 0) | tostring),
      "target M:  " + ((.summary.target_mod // .target_mod // 0) | tostring)
    end
  ' 2>/dev/null || echo "stats: fetch failed"
else
  echo "stats: install curl+jq"
fi

if systemctl is-active hackme-miner-worker.service >/dev/null 2>&1; then
  echo "worker service: active"
else
  echo "worker service: $(systemctl is-active hackme-miner-worker.service 2>/dev/null || echo unknown)"
fi

if command -v clinfo >/dev/null 2>&1; then
  echo "--- OpenCL ---"
  clinfo 2>/dev/null | awk '/Platform Name|Device Name|Number of devices/ {print}' | head -8
fi
if command -v nvidia-smi >/dev/null 2>&1; then
  echo "--- NVIDIA ---"
  nvidia-smi --query-gpu=name,driver_version,utilization.gpu --format=csv,noheader 2>/dev/null | head -3
fi
