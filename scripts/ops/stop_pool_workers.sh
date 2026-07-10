#!/usr/bin/env bash
# Stop all pool workers (autostart loop, workerpoh, node-managed worker API).
#
#   bash scripts/ops/stop_pool_workers.sh
#   ROOT_DIR=/path/to/HackMe bash scripts/ops/stop_pool_workers.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${ROOT_DIR:-${HACKME_ROOT:-$SCRIPT_DIR/../..}}" && pwd)"
LOG_DIR="${LOG_DIR:-$ROOT_DIR/logs}"
DESKTOP_ENV_FILE="${DESKTOP_ENV_FILE:-$ROOT_DIR/.env.desktop}"
ENV_FILE="${ENV_FILE:-$ROOT_DIR/.env}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
PAUSE_FILE="${HACKME_MINING_PAUSED_FILE:-$LOG_DIR/mining_paused}"

set -a
# shellcheck disable=SC1090
[[ -f "$DESKTOP_ENV_FILE" ]] && . "$DESKTOP_ENV_FILE"
[[ -f "$ENV_FILE" ]] && . "$ENV_FILE"
set +a

mkdir -p "$LOG_DIR"
date -u +%Y-%m-%dT%H:%M:%SZ >"$PAUSE_FILE"
chmod 600 "$PAUSE_FILE" 2>/dev/null || true

if [[ -n "${HACKME_ADMIN_TOKEN:-}" ]]; then
  curl -fsS -X POST "$BASE_URL/api/worker/stop" \
    -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" >/dev/null 2>&1 || true
fi

for pattern in \
  'scripts/ops/worker_autostart.sh' \
  'scripts/ops/worker_loop.sh' \
  'worker_autostart.sh' \
  'worker_loop.sh' \
  'workerpoh-cuda' \
  'workerpoh-opencl' \
  'workerpoh-cpu' \
  'bin/workerpoh' \
  'workerpoh '; do
  pkill -f "$pattern" 2>/dev/null || true
done
sleep 1
for pattern in 'workerpoh-cuda' 'workerpoh-opencl' 'workerpoh-cpu' 'workerpoh '; do
  pkill -9 -f "$pattern" 2>/dev/null || true
done

rm -f "${LOG_DIR}/.worker_autostart.lock" "$ROOT_DIR/logs/.worker_autostart.lock" 2>/dev/null || true

# Persist pause in env files (best-effort).
for ef in "$DESKTOP_ENV_FILE" "$ENV_FILE"; do
  [[ -f "$ef" ]] || continue
  for key in HACKME_WORKER_WATCHDOG WORKER_AUTOSTART; do
    if grep -q "^${key}=" "$ef" 2>/dev/null; then
      sed -i "s/^${key}=.*/${key}=0/" "$ef"
    else
      echo "${key}=0" >>"$ef"
    fi
  done
done

echo "[stop-workers] paused → $PAUSE_FILE"
remaining="$(pgrep -af 'workerpoh|worker_autostart|worker_loop' 2>/dev/null | grep -vE 'stop_pool_workers|pgrep|grep' || true)"
if [[ -n "$remaining" ]]; then
  echo "[stop-workers] WARN still running:"
  echo "$remaining"
  exit 1
fi
echo "[stop-workers] OK no worker processes"
