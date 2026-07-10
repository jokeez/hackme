#!/usr/bin/env bash
# Resume mining after desktop_mode_stop / stop_pool_workers (clears pause flag).
#
#   bash scripts/ops/resume_pool_mining.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${ROOT_DIR:-${HACKME_ROOT:-$SCRIPT_DIR/../..}}" && pwd)"
LOG_DIR="${LOG_DIR:-$ROOT_DIR/logs/desktop}"
DESKTOP_ENV_FILE="${DESKTOP_ENV_FILE:-$ROOT_DIR/.env.desktop}"
ENV_FILE="${ENV_FILE:-$ROOT_DIR/.env}"
PAUSE_FILE="${HACKME_MINING_PAUSED_FILE:-$LOG_DIR/mining_paused}"

rm -f "$PAUSE_FILE" "$ROOT_DIR/logs/desktop/mining_paused" 2>/dev/null || true

for ef in "$DESKTOP_ENV_FILE" "$ENV_FILE"; do
  [[ -f "$ef" ]] || continue
  for key in HACKME_WORKER_WATCHDOG WORKER_AUTOSTART; do
    if grep -q "^${key}=" "$ef" 2>/dev/null; then
      sed -i "s/^${key}=.*/${key}=1/" "$ef"
    else
      echo "${key}=1" >>"$ef"
    fi
  done
done

if systemctl --user is-enabled hackme-desktop.service >/dev/null 2>&1; then
  echo "[resume-mining] systemctl --user start hackme-desktop.service"
  systemctl --user start hackme-desktop.service || true
  sleep 3
fi

if [[ -x "$ROOT_DIR/scripts/ops/desktop_worker_reset.sh" ]]; then
  FORCE_MINING=1 bash "$ROOT_DIR/scripts/ops/desktop_worker_reset.sh"
elif [[ -x "$ROOT_DIR/desktop_worker_reset.sh" ]]; then
  FORCE_MINING=1 bash "$ROOT_DIR/desktop_worker_reset.sh"
elif [[ -x "$ROOT_DIR/scripts/ops/desktop_mode_up.sh" ]]; then
  WORKER_AUTOSTART=1 bash "$ROOT_DIR/scripts/ops/desktop_mode_up.sh"
else
  echo "[resume-mining] pause cleared; start miner manually (desktop_mode_up or start_hackme_miner.sh)"
fi
