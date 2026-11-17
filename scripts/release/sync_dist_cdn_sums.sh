#!/usr/bin/env bash
# Copy per-release SHA256SUMS(+iso) to dist/ CDN convenience roots with a note header.
# Per-release paths remain authoritative; root files are current-channel only.
#
#   bash scripts/release/sync_dist_cdn_sums.sh
#   VERSION=0.1.0-rc21 bash scripts/release/sync_dist_cdn_sums.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION" 2>/dev/null || true)}"
DIST="${DIST_DIR:-${ROOT}/dist/release_${VERSION}}"
[[ -n "$VERSION" && -d "$DIST" ]] || { echo "[sync-cdn-sums] set VERSION or DIST_DIR" >&2; exit 2; }
[[ -f "${DIST}/SHA256SUMS.txt" ]] || { echo "[sync-cdn-sums] missing ${DIST}/SHA256SUMS.txt" >&2; exit 2; }

mkdir -p "${ROOT}/dist"
{
  printf '%s\n' \
    '# HackMe release digests (CDN convenience copy).' \
    "# Authoritative per-release files: dist/release_${VERSION}/SHA256SUMS.txt" \
    "# Current channel: ${VERSION} — do not treat this root file as historical." \
    '#'
  # Strip any prior convenience headers if re-syncing from a root-style file.
  grep -v '^#' "${DIST}/SHA256SUMS.txt" || true
} >"${ROOT}/dist/SHA256SUMS.txt"

if [[ -f "${DIST}/SHA256SUMS-iso.txt" ]]; then
  {
    printf '%s\n' \
      '# HackMe OS ISO digests (CDN convenience copy).' \
      "# Authoritative: dist/release_${VERSION}/SHA256SUMS-iso.txt" \
      "# Current channel: ${VERSION}" \
      '#'
    grep -v '^#' "${DIST}/SHA256SUMS-iso.txt" || true
  } >"${ROOT}/dist/SHA256SUMS-iso.txt"
fi

# Tiny pointer for operators browsing the CDN tree
printf '%s\n' \
  '# HackMe dist/' \
  '' \
  'Per-release paths are authoritative:' \
  '' \
  '- `dist/release_<VERSION>/SHA256SUMS.txt`' \
  '- `dist/release_<VERSION>/SHA256SUMS-iso.txt`' \
  '- `dist/release_<VERSION>/latest.json`' \
  '' \
  "Root \`dist/SHA256SUMS*.txt\` and \`dist/latest.json\` track the **current** channel (\`${VERSION}\`)." \
  'Prefer the versioned directory when verifying a specific release.' \
  '' \
  'L1 update feed: `https://hackme.tech/dist/latest.json` (+ optional `latest.json.sig` ed25519).' \
  >"${ROOT}/dist/README.md"

echo "[sync-cdn-sums] ${ROOT}/dist/SHA256SUMS.txt ← release_${VERSION}"
[[ -f "${ROOT}/dist/SHA256SUMS-iso.txt" ]] && echo "[sync-cdn-sums] ${ROOT}/dist/SHA256SUMS-iso.txt ← release_${VERSION}"
