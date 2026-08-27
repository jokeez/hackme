#!/usr/bin/env bash
# Copy L1 updaters into an already-built dist/release_*/{linux,windows}/ tree
# WITHOUT retarring (published SHA256SUMS stay valid). Next make_release_bundle
# embeds them into archives automatically.
#
#   bash scripts/release/inject_updaters_into_dist.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION")}"
DIST="${1:-${ROOT}/dist/release_${VERSION}}"
[[ -d "$DIST" ]] || { echo "[inject] missing $DIST" >&2; exit 2; }

if [[ -d "${DIST}/linux" ]]; then
  install -m 0755 "${ROOT}/scripts/ops/update_hackme_miner.sh" "${DIST}/linux/update_hackme_miner.sh"
  install -m 0755 "${ROOT}/scripts/ops/update_hackme_os_binaries.sh" "${DIST}/linux/update_hackme_os_binaries.sh"
  install -m 0755 "${ROOT}/scripts/release/linux/install_menu_entry.sh" "${DIST}/linux/install_menu_entry.sh"
  install -m 0644 "${ROOT}/scripts/release/linux/hackme.desktop.template" "${DIST}/linux/hackme.desktop.template"
  install -m 0644 "${ROOT}/scripts/release/linux/hackme-dashboard.desktop.template" "${DIST}/linux/hackme-dashboard.desktop.template"
  mkdir -p "${DIST}/linux/icons"
  cp -a "${ROOT}/scripts/release/linux/icons/." "${DIST}/linux/icons/"
  echo "[inject] linux/ updaters + menu/icons"
fi
if [[ -d "${DIST}/windows" ]]; then
  install -m 0644 "${ROOT}/scripts/ops/update_hackme_miner.ps1" "${DIST}/windows/update_hackme_miner.ps1"
  install -m 0644 "${ROOT}/scripts/ops/update_hackme_miner.bat" "${DIST}/windows/update_hackme_miner.bat"
  echo "[inject] windows/ updaters"
fi
bash "${ROOT}/scripts/release/generate_latest_json.sh" "$DIST" || true
echo "[inject] done (archives unchanged — rebuild release to ship inside tar/zip/Setup)"
