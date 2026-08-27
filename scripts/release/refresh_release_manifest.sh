#!/usr/bin/env bash
# Regenerate RELEASE_MANIFEST.json + SHA256SUMS-iso.txt from dist/ (after ISO rebuild).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION" 2>/dev/null || echo 0.1.0-rc11m)}"
DIST_DIR="${1:-${ROOT}/dist/release_${VERSION}}"

if [[ ! -d "$DIST_DIR" ]]; then
  echo "[manifest] missing: $DIST_DIR" >&2
  exit 2
fi

APP_NAME="HackMe"
COMMIT_SHA="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_DATE_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

cd "$DIST_DIR"
[[ -f SHA256SUMS.txt ]] || { echo "[manifest] run make_release_bundle first" >&2; exit 1; }

ISO_OS="HackMe-OS-${VERSION}-amd64.iso"
if [[ -f "$ISO_OS" ]]; then
  sha256sum "$(basename "$ISO_OS")" > SHA256SUMS-iso.txt
  chmod 644 SHA256SUMS-iso.txt 2>/dev/null || true
fi

WIN_INSTALLER="HackMe-Setup-${VERSION}.exe"
if [[ -f "$WIN_INSTALLER" ]]; then
  WIN_PRIMARY="$WIN_INSTALLER"
  WIN_SHA="$(awk '/HackMe-Setup-.*\.exe$/ {print $1}' SHA256SUMS.txt)"
else
  WIN_PRIMARY="hackme_${VERSION}_windows_setup.zip"
  WIN_SHA="$(awk '/windows_setup\.zip$/ {print $1}' SHA256SUMS.txt)"
fi
LINUX_ARCHIVE="hackme_${VERSION}_linux.tar.gz"
LINUX_SHA="$(awk '/linux\.tar\.gz$/ {print $1}' SHA256SUMS.txt)"
WIN_SIZE="$(stat -c%s "$WIN_PRIMARY")"
LINUX_SIZE="$(stat -c%s "$LINUX_ARCHIVE")"

ISO_FILE=""
ISO_SHA=""
ISO_SIZE=0
if [[ -f "$ISO_OS" ]]; then
  ISO_FILE="$ISO_OS"
  ISO_SHA="$(awk -v f="$ISO_FILE" '$2==f || $NF==f {print $1; exit}' SHA256SUMS-iso.txt)"
  ISO_SIZE="$(stat -c%s "$ISO_FILE")"
fi

jq -nc \
  --arg app "$APP_NAME" \
  --arg version "$VERSION" \
  --arg commit "$COMMIT_SHA" \
  --arg build_date_utc "$BUILD_DATE_UTC" \
  --arg windows_file "$(basename "$WIN_PRIMARY")" \
  --arg windows_sha256 "$WIN_SHA" \
  --argjson windows_size_bytes "$WIN_SIZE" \
  --arg linux_file "$(basename "$LINUX_ARCHIVE")" \
  --arg linux_sha256 "$LINUX_SHA" \
  --argjson linux_size_bytes "$LINUX_SIZE" \
  --argjson has_installer "$( [[ -f "$WIN_INSTALLER" ]] && echo true || echo false )" \
  --arg iso_file "$ISO_FILE" \
  --arg iso_sha256 "$ISO_SHA" \
  --argjson iso_size_bytes "$ISO_SIZE" \
  '{
    app:$app,
    version:$version,
    commit:$commit,
    build_date_utc:$build_date_utc,
    windows_installer:$has_installer,
    hackme_os_iso: ($iso_file != ""),
    artifacts: (
      [
        {platform:"windows",file:$windows_file,sha256:$windows_sha256,size_bytes:$windows_size_bytes,kind:(if $has_installer then "installer" else "zip" end)},
        {platform:"linux",file:$linux_file,sha256:$linux_sha256,size_bytes:$linux_size_bytes}
      ]
      + (if $iso_file != "" then [{platform:"hackme-os",file:$iso_file,sha256:$iso_sha256,size_bytes:$iso_size_bytes,kind:"iso",features:["zero_knowledge_start","live_usb","noplymouth_console"]}] else [] end)
    )
  }' > RELEASE_MANIFEST.json

echo "[manifest] OK $(pwd)/RELEASE_MANIFEST.json (iso_sha=${ISO_SHA:-none})"

VERSION="${VERSION}" bash "${ROOT}/scripts/release/generate_latest_json.sh" "${DIST_DIR}" || \
  echo "[manifest] WARN: latest.json refresh failed" >&2
