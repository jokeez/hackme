#!/usr/bin/env bash
# Golden path — one command: detect GPU → start node → start pool worker → wait for coordinator row.
#
# Linux (dev checkout):
#   cp .env.desktop.example .env.desktop   # once: WORKER_ID, tokens, payout map
#   bash scripts/ops/start_pool_miner.sh
#
# Linux (release tarball): bash start_hackme_miner.sh  (same flow, prebuilt binaries)
# Windows: Start menu → "HackMe Miner" (start_hackme_miner.bat)
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

DESKTOP_ENV_FILE="${DESKTOP_ENV_FILE:-$ROOT_DIR/.env.desktop}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
WAIT_SEC="${WAIT_SEC:-120}"
NODE_ONLY=0
WORKER_ONLY=0

usage() {
  cat <<EOF
Usage: bash scripts/ops/start_pool_miner.sh [--node-only|--worker-only]

  default     start node (if needed) + GPU worker + wait for pool row
  --node-only skip worker (desktop_mode_up only)
  --worker-only  skip node start (worker reset + pool wait)
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --node-only) NODE_ONLY=1; shift ;;
    --worker-only) WORKER_ONLY=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "[golden] unknown arg: $1" >&2; usage; exit 2 ;;
  esac
done

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[golden] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq

if [[ ! -f "$DESKTOP_ENV_FILE" ]]; then
  if [[ -f "$ROOT_DIR/.env.desktop.example" ]]; then
    echo "[golden] creating $DESKTOP_ENV_FILE from example (edit WORKER_ID + tokens)"
    cp "$ROOT_DIR/.env.desktop.example" "$DESKTOP_ENV_FILE"
  else
    echo "[golden] missing $DESKTOP_ENV_FILE — copy .env.desktop.example first" >&2
    exit 2
  fi
fi

set -a
# shellcheck disable=SC1090
source "$DESKTOP_ENV_FILE"
set +a

WORKER_ID="${WORKER_ID:-worker-$(hostname -s 2>/dev/null || echo pc)}"
COORD_URL="${HACKME_POOL_COORDINATOR_URL:-$COORD_URL}"

GPU_BACKEND="${HACKME_GPU_BACKEND:-auto}"
if [[ "$GPU_BACKEND" == "auto" || -z "$GPU_BACKEND" ]]; then
  GPU_BACKEND="$(HACKME_REPO_ROOT="$ROOT_DIR" bash "$ROOT_DIR/scripts/ops/detect_gpu_backend.sh" 2>/dev/null || echo cpu)"
fi
echo "[golden] worker_id=$WORKER_ID gpu_backend=$GPU_BACKEND coord=$COORD_URL"

if [[ "$WORKER_ONLY" -eq 0 ]]; then
  echo "[golden] starting desktop node..."
  WORKER_AUTOSTART=0 DESKTOP_PROFILE=worker bash "$ROOT_DIR/scripts/ops/desktop_mode_up.sh"
fi

if [[ "$NODE_ONLY" -eq 1 ]]; then
  echo "[golden] node-only — done ($BASE_URL)"
  exit 0
fi

if ! curl -fsS "$BASE_URL/api/status" >/dev/null 2>&1; then
  echo "[golden] node not healthy at $BASE_URL" >&2
  exit 1
fi

echo "[golden] starting pool worker (detect → autostart)..."
bash "$ROOT_DIR/scripts/ops/desktop_worker_reset.sh"

echo "[golden] waiting for coordinator row (up to ${WAIT_SEC}s)..."
deadline=$((SECONDS + WAIT_SEC))
online=0
gh=0
while [[ "$SECONDS" -lt "$deadline" ]]; do
  # Prefer local proxy (same view as dashboard)
  if row="$(curl -fsS "$BASE_URL/api/work/stats?details=0" 2>/dev/null | jq -c --arg w "$WORKER_ID" '.workers[$w] // empty' 2>/dev/null)" && [[ -n "$row" && "$row" != "null" ]]; then
    online="$(echo "$row" | jq -r '.online // false')"
    gh="$(echo "$row" | jq -r '.hashrate_gh_s // 0')"
    if [[ "$(echo "$gh" | awk '{print ($1+0)>0}')" == 1 ]]; then
      echo "[golden] PASS: $WORKER_ID online pool GH/s=$(echo "$gh" | awk '{printf "%.2f", $1+0}')"
      echo "[golden] dashboard: $BASE_URL/#ecosystem"
      exit 0
    fi
  fi
  if pgrep -af 'workerpoh-' >/dev/null 2>&1; then
    echo "[golden] worker process up; coordinator row pending..."
  fi
  sleep 3
done

echo "[golden] WARN: timeout — worker may still be warming up" >&2
echo "[golden] check: curl -s $BASE_URL/api/worker/status | jq ." >&2
echo "[golden] logs: tail -f logs/worker_participant.log" >&2
exit 1
