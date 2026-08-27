#!/usr/bin/env bash
# Local-only: inject L1 updaters into linux/, re-tar, refresh SHA256SUMS + latest.json.
# Does NOT publish to GitHub or the site.
#
#   bash scripts/release/repack_linux_with_updaters.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION")}"
DIST="${1:-${ROOT}/dist/release_${VERSION}}"
LINUX_DIR="${DIST}/linux"
TAR="${DIST}/hackme_${VERSION}_linux.tar.gz"
[[ -d "$LINUX_DIR" && -x "${LINUX_DIR}/hackme" ]] || { echo "[repack] need ${LINUX_DIR}/hackme" >&2; exit 2; }

install -m 0755 "${ROOT}/scripts/ops/update_hackme_miner.sh" "${LINUX_DIR}/update_hackme_miner.sh"
install -m 0755 "${ROOT}/scripts/ops/update_hackme_os_binaries.sh" "${LINUX_DIR}/update_hackme_os_binaries.sh"
install -m 0755 "${ROOT}/scripts/release/linux/install_menu_entry.sh" "${LINUX_DIR}/install_menu_entry.sh"
install -m 0644 "${ROOT}/scripts/release/linux/hackme.desktop.template" "${LINUX_DIR}/hackme.desktop.template"
install -m 0644 "${ROOT}/scripts/release/linux/hackme-dashboard.desktop.template" "${LINUX_DIR}/hackme-dashboard.desktop.template"
mkdir -p "${LINUX_DIR}/icons"
cp -a "${ROOT}/scripts/release/linux/icons/." "${LINUX_DIR}/icons/"
if [[ -d "${DIST}/windows" ]]; then
  install -m 0644 "${ROOT}/scripts/ops/update_hackme_miner.ps1" "${DIST}/windows/update_hackme_miner.ps1"
  install -m 0644 "${ROOT}/scripts/ops/update_hackme_miner.bat" "${DIST}/windows/update_hackme_miner.bat"
fi

(
  cd "$DIST"
  tar -czf "hackme_${VERSION}_linux.tar.gz" "linux"
  echo "[repack] wrote hackme_${VERSION}_linux.tar.gz"
)

# Refresh SHA256SUMS.txt lines for the linux tar (keep other lines)
SUMS="${DIST}/SHA256SUMS.txt"
if [[ -f "$SUMS" ]]; then
  NEW_SHA="$(sha256sum "$TAR" | awk '{print $1}')"
  BASE="$(basename "$TAR")"
  tmp="$(mktemp)"
  awk -v f="$BASE" -v s="$NEW_SHA" '
    $NF==f || $2==f { print s "  " f; next }
    { print }
  ' "$SUMS" >"$tmp"
  if ! grep -qE "(^| )${BASE}$" "$tmp"; then
    echo "${NEW_SHA}  ${BASE}" >>"$tmp"
  fi
  mv "$tmp" "$SUMS"
  echo "[repack] SHA256SUMS updated for $BASE → ${NEW_SHA:0:16}…"
fi

VERSION="$VERSION" bash "${ROOT}/scripts/release/generate_latest_json.sh" "$DIST"
echo "[repack] local only — not published"
