#!/usr/bin/env bash
set -euo pipefail

# Installs HackMe Desktop Mode launcher into Linux app menu (dev tree).

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ICON_SRC="${ROOT_DIR}/scripts/release/linux/icons"

DESKTOP_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
ICON_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/256x256/apps"
DESKTOP_FILE="$DESKTOP_DIR/hackme-desktop.desktop"
ICON_FILE="$ICON_DIR/hackme-desktop.png"

mkdir -p "$DESKTOP_DIR" "$ICON_DIR"

if [[ -f "$ICON_SRC/hackme-256.png" ]]; then
  cp "$ICON_SRC/hackme-256.png" "$ICON_FILE"
elif [[ -f "$ICON_SRC/hackme.png" ]]; then
  cp "$ICON_SRC/hackme.png" "$ICON_FILE"
elif [[ -f "$ROOT_DIR/web/site/assets/logo-hex.png" ]]; then
  cp "$ROOT_DIR/web/site/assets/logo-hex.png" "$ICON_FILE"
fi

cat >"$DESKTOP_FILE" <<EOF
[Desktop Entry]
Version=1.0
Type=Application
Name=HackMe Desktop
Comment=HackMe command node desktop mode
Exec=/usr/bin/env bash -lc 'cd "$ROOT_DIR" && bash scripts/ops/desktop_mode_up.sh'
Icon=hackme-desktop
Terminal=false
Categories=Development;Security;
StartupNotify=true
EOF

chmod +x "$DESKTOP_FILE"

# Also offer production-style HackMe menu if release icons exist
if [[ -x "${ROOT_DIR}/scripts/release/linux/install_menu_entry.sh" ]]; then
  bash "${ROOT_DIR}/scripts/release/linux/install_menu_entry.sh" --user \
    --install-dir "${ROOT_DIR}" --payload-dir "${ROOT_DIR}/scripts/release/linux" 2>/dev/null || true
fi

echo "[desktop-launcher] installed: $DESKTOP_FILE"
echo "[desktop-launcher] open from app menu: HackMe Desktop"
