#!/usr/bin/env bash
# Install HackMe branded menu entry + icons (system or user).
# Used by install_hackme.sh and deb postinst.
#
#   INSTALL_DIR=/opt/hackme bash scripts/release/linux/install_menu_entry.sh
#   bash install_menu_entry.sh --user   # ~/.local/share
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${INSTALL_DIR:-/opt/hackme}"
MODE=system
while [[ $# -gt 0 ]]; do
  case "$1" in
    --user) MODE=user; shift ;;
    --install-dir) INSTALL_DIR="${2:-}"; shift 2 ;;
    --payload-dir) PAYLOAD_DIR="${2:-}"; shift 2 ;;
    *) echo "[menu] unknown arg: $1" >&2; exit 2 ;;
  esac
done
PAYLOAD_DIR="${PAYLOAD_DIR:-$HERE}"

if [[ "$MODE" == "user" ]]; then
  APP_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
  ICON_BASE="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor"
else
  APP_DIR="/usr/share/applications"
  ICON_BASE="/usr/share/icons/hicolor"
fi

mkdir -p "$APP_DIR"
for sz in 48 128 256; do
  mkdir -p "${ICON_BASE}/${sz}x${sz}/apps"
done

pick_icon() {
  local name="$1"
  if [[ -f "${PAYLOAD_DIR}/icons/${name}" ]]; then
    echo "${PAYLOAD_DIR}/icons/${name}"
  elif [[ -f "${PAYLOAD_DIR}/${name}" ]]; then
    echo "${PAYLOAD_DIR}/${name}"
  elif [[ -f "${INSTALL_DIR}/icons/${name}" ]]; then
    echo "${INSTALL_DIR}/icons/${name}"
  elif [[ -f "${INSTALL_DIR}/${name}" ]]; then
    echo "${INSTALL_DIR}/${name}"
  else
    echo ""
  fi
}

I48="$(pick_icon hackme-48.png)"
I128="$(pick_icon hackme-128.png)"
I256="$(pick_icon hackme-256.png)"
IFULL="$(pick_icon hackme.png)"
[[ -n "$I256" || -n "$IFULL" ]] || {
  echo "[menu] WARN: no hackme icon found — desktop entry will use generic name Icon=hackme" >&2
}
[[ -n "$I48" ]] && install -m 0644 "$I48" "${ICON_BASE}/48x48/apps/hackme.png"
[[ -n "$I128" ]] && install -m 0644 "$I128" "${ICON_BASE}/128x128/apps/hackme.png"
if [[ -n "$I256" ]]; then
  install -m 0644 "$I256" "${ICON_BASE}/256x256/apps/hackme.png"
elif [[ -n "$IFULL" ]]; then
  install -m 0644 "$IFULL" "${ICON_BASE}/256x256/apps/hackme.png"
fi
# Keep a copy next to binaries for portable / updater refresh (best-effort)
if mkdir -p "${INSTALL_DIR}/icons" 2>/dev/null; then
  for f in hackme.png hackme-48.png hackme-128.png hackme-256.png; do
    src="$(pick_icon "$f")"
    [[ -n "$src" ]] && install -m 0644 "$src" "${INSTALL_DIR}/icons/$f" 2>/dev/null || true
  done
fi

render_desktop() {
  local tpl="$1" out="$2"
  [[ -f "$tpl" ]] || return 0
  sed "s#__INSTALL_DIR__#${INSTALL_DIR}#g" "$tpl" >"$out"
  chmod 0644 "$out"
}

TPL_MAIN="${PAYLOAD_DIR}/hackme.desktop.template"
TPL_DASH="${PAYLOAD_DIR}/hackme-dashboard.desktop.template"
[[ -f "$TPL_MAIN" ]] || TPL_MAIN="${HERE}/hackme.desktop.template"
[[ -f "$TPL_DASH" ]] || TPL_DASH="${HERE}/hackme-dashboard.desktop.template"

# Prefer start script; fall back to binary
if [[ ! -x "${INSTALL_DIR}/start_hackme_miner.sh" ]]; then
  if [[ -f "$TPL_MAIN" ]]; then
    sed "s#__INSTALL_DIR__/start_hackme_miner.sh#${INSTALL_DIR}/hackme#g" "$TPL_MAIN" \
      | sed "s#__INSTALL_DIR__#${INSTALL_DIR}#g" >"${APP_DIR}/hackme.desktop"
    chmod 0644 "${APP_DIR}/hackme.desktop"
  fi
else
  render_desktop "$TPL_MAIN" "${APP_DIR}/hackme.desktop"
fi
render_desktop "$TPL_DASH" "${APP_DIR}/hackme-dashboard.desktop"

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$APP_DIR" >/dev/null 2>&1 || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -f "$ICON_BASE" >/dev/null 2>&1 || true
fi

echo "[menu] installed: ${APP_DIR}/hackme.desktop (+ dashboard)"
echo "[menu] icons: ${ICON_BASE}/*/apps/hackme.png"
