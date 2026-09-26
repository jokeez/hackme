#!/usr/bin/env bash
# Install Hunt daily rotate as a systemd --user hourly timer.
# Usage:
#   bash scripts/ops/install_hunt_daily_rotate_user.sh
#   HACKME_REPO_ROOT=/path/to/hackme bash scripts/ops/install_hunt_daily_rotate_user.sh
#   SLOT_WALL_SEC=2700 bash scripts/ops/install_hunt_daily_rotate_user.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ROOT="${HACKME_REPO_ROOT:-$ROOT}"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
mkdir -p "$UNIT_DIR"

SLOT_WALL_SEC="${SLOT_WALL_SEC:-3600}"
HUNT_PKG="${HUNT_PKG:-hunt_standard}"

sed -e "s|%h/Desktop/hackme|$ROOT|g" \
  -e "s|Environment=SLOT_WALL_SEC=3600|Environment=SLOT_WALL_SEC=$SLOT_WALL_SEC|g" \
  -e "s|Environment=HUNT_PKG=hunt_standard|Environment=HUNT_PKG=$HUNT_PKG|g" \
  "$ROOT/scripts/ops/systemd/hackme-hunt-daily-rotate.service" >"$UNIT_DIR/hackme-hunt-daily-rotate.service"
cp "$ROOT/scripts/ops/systemd/hackme-hunt-daily-rotate.timer" "$UNIT_DIR/hackme-hunt-daily-rotate.timer"

chmod +x "$ROOT/scripts/ops/hunt_daily_rotate.sh"
systemctl --user daemon-reload
systemctl --user enable --now hackme-hunt-daily-rotate.timer
systemctl --user status hackme-hunt-daily-rotate.timer --no-pager || true

echo
echo "[install] timer enabled (hourly). Next:"
systemctl --user list-timers 'hackme-hunt-daily-rotate*' --no-pager || true
echo
echo "Manual one slot:  bash $ROOT/scripts/ops/hunt_daily_rotate.sh"
echo "Dry plan:         DRY_RUN=1 bash $ROOT/scripts/ops/hunt_daily_rotate.sh"
echo "Stop:             systemctl --user disable --now hackme-hunt-daily-rotate.timer"
echo "Logs:             journalctl --user -u hackme-hunt-daily-rotate.service -f"
echo "Rollups:          $ROOT/reports/hunt-daily/\$(date -u +%Y%m%d)/ROLLUP.md"
