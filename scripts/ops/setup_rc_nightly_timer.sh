#!/usr/bin/env bash
set -euo pipefail

# Install and enable nightly RC freeze timer on VPS.
#
# Usage:
#   sudo bash scripts/ops/setup_rc_nightly_timer.sh
#
# Optional:
#   UNIT_DIR=/etc/systemd/system

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
UNIT_DIR="${UNIT_DIR:-/etc/systemd/system}"
SERVICE_SRC="$ROOT_DIR/scripts/ops/systemd/hackme-rc-freeze-nightly.service"
TIMER_SRC="$ROOT_DIR/scripts/ops/systemd/hackme-rc-freeze-nightly.timer"
SERVICE_DST="$UNIT_DIR/hackme-rc-freeze-nightly.service"
TIMER_DST="$UNIT_DIR/hackme-rc-freeze-nightly.timer"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[rc-nightly-setup] missing command: $1" >&2
    exit 1
  }
}

require_cmd sudo
require_cmd systemctl
require_cmd install

if [[ ! -f "$SERVICE_SRC" || ! -f "$TIMER_SRC" ]]; then
  echo "[rc-nightly-setup] systemd templates not found under scripts/ops/systemd" >&2
  exit 2
fi

echo "[rc-nightly-setup] installing service and timer units"
sudo install -m 0644 "$SERVICE_SRC" "$SERVICE_DST"
sudo install -m 0644 "$TIMER_SRC" "$TIMER_DST"

echo "[rc-nightly-setup] daemon-reload"
sudo systemctl daemon-reload

echo "[rc-nightly-setup] enable + start timer"
sudo systemctl enable --now hackme-rc-freeze-nightly.timer

echo "[rc-nightly-setup] timer status"
sudo systemctl status --no-pager hackme-rc-freeze-nightly.timer || true
echo
echo "[rc-nightly-setup] next scheduled run:"
sudo systemctl list-timers --all --no-pager | awk 'NR==1 || /hackme-rc-freeze-nightly/'
echo
echo "[rc-nightly-setup] manual run:"
echo "  sudo systemctl start hackme-rc-freeze-nightly.service"
