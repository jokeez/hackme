#!/usr/bin/env bash
# Fast upgrade for hackme-node — same multi-mirror download as install.sh
# (does not use slow `apt upgrade` Cloudflare fetch for the .deb blob).
#
#   curl -fsSL https://hackme.tech/apt/upgrade.sh | sudo bash
set -euo pipefail

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "[hackme-apt] run as root: curl -fsSL https://hackme.tech/apt/upgrade.sh | sudo bash" >&2
  exit 1
fi

APT_BASE="${HACKME_APT_BASE:-https://hackme.tech/apt}"
APT_BASE="${APT_BASE%/}"
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
curl -fsSL "${APT_BASE}/install.sh" -o "$TMP"
chmod +x "$TMP"
echo "[hackme-apt] upgrade via install.sh mirrors…"
bash "$TMP"
