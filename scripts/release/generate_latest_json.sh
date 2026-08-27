#!/usr/bin/env bash
# Generate dist/release_<VER>/latest.json (+ optional web/site mirror hint).
# Usage:
#   VERSION=0.1.0-rc15 bash scripts/release/generate_latest_json.sh
#   bash scripts/release/generate_latest_json.sh /path/to/dist/release_0.1.0-rc15
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION" 2>/dev/null || true)}"
DIST_DIR="${1:-}"
if [[ -z "$DIST_DIR" ]]; then
  [[ -n "$VERSION" ]] || { echo "[latest] set VERSION or pass DIST_DIR" >&2; exit 2; }
  DIST_DIR="${ROOT}/dist/release_${VERSION}"
fi
[[ -d "$DIST_DIR" ]] || { echo "[latest] missing dist: $DIST_DIR" >&2; exit 2; }
[[ -f "${DIST_DIR}/SHA256SUMS.txt" ]] || { echo "[latest] missing SHA256SUMS.txt" >&2; exit 2; }

if [[ -z "$VERSION" ]]; then
  VERSION="$(basename "$DIST_DIR" | sed 's/^release_//')"
fi

# Public download bases (GitHub primary; site CDN mirror).
GH_BASE="${HACKME_RELEASE_BASE_URL:-https://github.com/jokeez/hackme/releases/download/${VERSION}}"
SITE_BASE="${HACKME_SITE_DIST_BASE_URL:-https://hackme.tech/dist/release_${VERSION}}"

sha_of() {
  local f="$1"
  awk -v name="$f" '$2 == name || $NF == name { print $1; exit }' "${DIST_DIR}/SHA256SUMS.txt"
}

size_of() {
  local p="${DIST_DIR}/$1"
  [[ -f "$p" ]] || { echo 0; return; }
  stat -c%s "$p" 2>/dev/null || stat -f%z "$p"
}

COMMIT="$(jq -r '.commit // empty' "${DIST_DIR}/RELEASE_MANIFEST.json" 2>/dev/null || true)"
[[ -n "$COMMIT" ]] || COMMIT="$(git -C "$ROOT" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
BUILD_DATE="$(jq -r '.build_date_utc // empty' "${DIST_DIR}/RELEASE_MANIFEST.json" 2>/dev/null || true)"
[[ -n "$BUILD_DATE" ]] || BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

LINUX_FILE="hackme_${VERSION}_linux.tar.gz"
WIN_SETUP="HackMe-Setup-${VERSION}.exe"
WIN_ZIP="hackme_${VERSION}_windows.zip"
ISO_FILE="HackMe-OS-${VERSION}-amd64.iso"

LINUX_SHA="$(sha_of "$LINUX_FILE")"
WIN_SETUP_SHA="$(sha_of "$WIN_SETUP")"
WIN_ZIP_SHA="$(sha_of "$WIN_ZIP")"
ISO_SHA=""
if [[ -f "${DIST_DIR}/SHA256SUMS-iso.txt" ]]; then
  ISO_SHA="$(awk -v name="$ISO_FILE" '$2 == name || $NF == name { print $1; exit }' "${DIST_DIR}/SHA256SUMS-iso.txt")"
fi

[[ -n "$LINUX_SHA" && -f "${DIST_DIR}/${LINUX_FILE}" ]] || {
  echo "[latest] linux archive missing or no sha: $LINUX_FILE" >&2
  exit 1
}

platforms='[]'
add_plat() {
  local id="$1" file="$2" sha="$3" kind="$4"
  [[ -n "$sha" && -f "${DIST_DIR}/${file}" ]] || return 0
  local sz
  sz="$(size_of "$file")"
  platforms="$(jq -c \
    --arg id "$id" --arg file "$file" --arg sha "$sha" --arg kind "$kind" \
    --arg url "${GH_BASE}/${file}" --arg mirror "${SITE_BASE}/${file}" \
    --argjson size "$sz" \
    '. + [{
      id:$id, file:$file, sha256:$sha, size_bytes:$size, kind:$kind,
      url:$url, mirror_url:$mirror
    }]' <<<"$platforms")"
}

add_plat "linux" "$LINUX_FILE" "$LINUX_SHA" "tar.gz"
add_plat "windows_installer" "$WIN_SETUP" "$WIN_SETUP_SHA" "installer"
add_plat "windows_zip" "$WIN_ZIP" "$WIN_ZIP_SHA" "zip"
DEB_FILE="hackme-node_${VERSION}_amd64.deb"
if [[ -f "${DIST_DIR}/${DEB_FILE}" ]]; then
  DEB_SHA="$(sha256sum "${DIST_DIR}/${DEB_FILE}" | awk '{print $1}')"
  add_plat "linux_deb" "$DEB_FILE" "$DEB_SHA" "deb"
fi
if [[ -n "$ISO_SHA" && -f "${DIST_DIR}/${ISO_FILE}" ]]; then
  sz="$(stat -c%s "${DIST_DIR}/${ISO_FILE}" 2>/dev/null || echo 0)"
  platforms="$(jq -c \
    --arg file "$ISO_FILE" --arg sha "$ISO_SHA" \
    --arg url "${GH_BASE}/${ISO_FILE}" --arg mirror "${SITE_BASE}/${ISO_FILE}" \
    --argjson size "$sz" \
    '. + [{
      id:"hackme_os_iso", file:$file, sha256:$sha, size_bytes:$size, kind:"iso",
      url:$url, mirror_url:$mirror, update:"full_iso_rare"
    }]' <<<"$platforms")"
fi

OUT="${DIST_DIR}/latest.json"
jq -nc \
  --arg schema "hackme.release.latest.v1" \
  --arg app "HackMe" \
  --arg version "$VERSION" \
  --arg commit "$COMMIT" \
  --arg build_date_utc "$BUILD_DATE" \
  --arg channel "stable" \
  --arg min_updater "1" \
  --argjson platforms "$platforms" \
  --arg notes "Replace binaries only; never overwrite .env, data/, logs/, wallet seeds." \
  '{
    schema: $schema,
    app: $app,
    version: $version,
    commit: $commit,
    build_date_utc: $build_date_utc,
    channel: $channel,
    min_updater: ($min_updater|tonumber),
    preserve: [".env", ".env.desktop", ".env.vps", "data", "logs", "wallet", "*.seed", "pool.miner.token"],
    platforms: $platforms,
    notes: $notes
  }' >"$OUT"

# Convenience copy for site CDN root (https://hackme.tech/dist/latest.json)
mkdir -p "${ROOT}/dist"
cp -f "$OUT" "${ROOT}/dist/latest.json"

echo "[latest] wrote $OUT"
echo "[latest] also ${ROOT}/dist/latest.json (CDN root hint)"
jq -r '"version=\(.version) platforms=\(.platforms|length) linux=\(.platforms[]|select(.id=="linux")|.sha256[0:16])…"' "$OUT"
