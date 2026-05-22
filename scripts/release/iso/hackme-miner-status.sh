#!/usr/bin/env bash
# Console-friendly pool + GPU status (RX 580 / NVIDIA, hackme.tech pool).
set -euo pipefail

UI="/opt/hackme/scripts/release/iso/hackme-os-ui.sh"
[[ -f "$UI" ]] && source "$UI"

ENV_STATE="/var/lib/hackme/miner.env"
[[ -f /etc/hackme/miner.env ]] && source /etc/hackme/miner.env
[[ -f "$ENV_STATE" ]] && source "$ENV_STATE"

COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
WORKER_ID="${WORKER_ID:-unknown}"
POOL_LABEL="hackme.tech public pool (FirstVDS coordinator)"

if declare -f hackme_ui_status_dashboard >/dev/null 2>&1; then
  hackme_ui_status_dashboard "$COORD_URL" "$WORKER_ID" "$POOL_LABEL"
  echo ""
  if [[ -f /run/hackme-os/topology.json ]]; then
    echo "${HM_DIM}topology:${HM_RST} $(cat /run/hackme-os/topology.json)"
  fi
  if command -v clinfo >/dev/null 2>&1; then
    echo "${HM_DIM}--- OpenCL ---${HM_RST}"
    clinfo 2>/dev/null | awk '/Platform Name|Device Name|Number of devices/ {print}' | head -8
  fi
  exit 0
fi

echo "=== HackMe OS $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
echo "worker: ${WORKER_ID}"
echo "pool:   ${COORD_URL}"
