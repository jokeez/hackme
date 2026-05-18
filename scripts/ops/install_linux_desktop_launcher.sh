#!/usr/bin/env bash
set -euo pipefail

# Installs HackMe Desktop Mode launcher into Linux app menu.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DESKTOP_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
ICON_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/256x256/apps"
DESKTOP_FILE="$DESKTOP_DIR/hackme-desktop.desktop"
ICON_FILE="$ICON_DIR/hackme-desktop.png"

mkdir -p "$DESKTOP_DIR" "$ICON_DIR"

if [[ -f "$ROOT_DIR/web/site/assets/logo-hex.png" ]]; then
  cp "$ROOT_DIR/web/site/assets/logo-hex.png" "$ICON_FILE"
fi

cat >"$DESKTOP_FILE" <<EOF
[Desktop Entry]
Version=1.0
Type=Application
Name=HackMe Desktop
Comment=HackMe command node desktop mode
Exec=/usr/bin/env bash -lc 'cd "$ROOT_DIR" && bash scripts/ops/desktop_mode_up.sh'
Icon=${ICON_FILE}
Terminal=false
Categories=Development;Security;
StartupNotify=true
EOF

chmod +x "$DESKTOP_FILE"

echo "[desktop-launcher] installed: $DESKTOP_FILE"
echo "[desktop-launcher] open from app menu: HackMe Desktop"
