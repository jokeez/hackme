#!/usr/bin/env bash
# Install hourly-ish D1 soak observer as a user systemd timer.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
mkdir -p "$UNIT_DIR"
chmod +x "$ROOT/scripts/ops/d1_soak_watch.sh"
cp "$ROOT/scripts/ops/systemd/hackme-d1-soak-watch.service" "$UNIT_DIR/"
cp "$ROOT/scripts/ops/systemd/hackme-d1-soak-watch.timer" "$UNIT_DIR/"
# Rewrite WorkingDirectory / ExecStart to this checkout.
sed -i "s|%h/Desktop/HackMe|$ROOT|g" "$UNIT_DIR/hackme-d1-soak-watch.service"
systemctl --user daemon-reload
systemctl --user enable --now hackme-d1-soak-watch.timer
systemctl --user start hackme-d1-soak-watch.service || true
systemctl --user list-timers hackme-d1-soak-watch.timer --no-pager || true
echo "Log: $ROOT/logs/d1-soak/watch.jsonl"
echo "Stop: systemctl --user disable --now hackme-d1-soak-watch.timer"
