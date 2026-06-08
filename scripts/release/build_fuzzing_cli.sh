#!/usr/bin/env bash
# Build hackme-fuzzing CLI (B2B integrators) for Linux + Windows into dist/release_<VERSION>/.
#
#   VERSION=0.1.0-rc11m bash scripts/release/build_fuzzing_cli.sh
#   NODE_SSH=root@132.243.112.100 bash scripts/release/build_fuzzing_cli.sh --deploy
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION" 2>/dev/null || echo 0.1.0-rc11m)}"
DIST_DIR="${DIST_DIR:-${ROOT}/dist/release_${VERSION}}"
DEPLOY="${DEPLOY:-0}"

for arg in "$@"; do
  [[ "$arg" == "--deploy" ]] && DEPLOY=1
done

[[ -d "$DIST_DIR" ]] || mkdir -p "$DIST_DIR"

LINUX_OUT="${DIST_DIR}/hackme-fuzzing-${VERSION}-linux-amd64"
WIN_OUT="${DIST_DIR}/hackme-fuzzing-${VERSION}-windows-amd64.exe"
LDFLAGS="-s -w"

echo "[fuzzing-cli] build linux → $(basename "$LINUX_OUT")"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "$LDFLAGS" -o "$LINUX_OUT" ./cmd/fuzzingclient
chmod 755 "$LINUX_OUT"

echo "[fuzzing-cli] build windows → $(basename "$WIN_OUT")"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "$LDFLAGS" -o "$WIN_OUT" ./cmd/fuzzingclient

# Refresh SHA256SUMS.txt (keep existing lines, update fuzzing entries)
(
  cd "$DIST_DIR"
  mapfile -t existing < <(grep -v 'hackme-fuzzing-' SHA256SUMS.txt 2>/dev/null || true)
  {
    for line in "${existing[@]}"; do echo "$line"; done
    sha256sum "$(basename "$LINUX_OUT")" "$(basename "$WIN_OUT")"
  } > SHA256SUMS.txt.new
  mv SHA256SUMS.txt.new SHA256SUMS.txt
)

LINUX_SHA="$(sha256sum "$LINUX_OUT" | awk '{print $1}')"
WIN_SHA="$(sha256sum "$WIN_OUT" | awk '{print $1}')"
LINUX_SIZE="$(stat -c%s "$LINUX_OUT")"
WIN_SIZE="$(stat -c%s "$WIN_OUT")"
COMMIT_SHA="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_DATE_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Patch RELEASE_MANIFEST.json — add/update fuzzing artifacts
MANIFEST="${DIST_DIR}/RELEASE_MANIFEST.json"
if [[ -f "$MANIFEST" ]]; then
  jq --arg v "$VERSION" \
    --arg linux_f "$(basename "$LINUX_OUT")" --arg linux_sha "$LINUX_SHA" --argjson linux_sz "$LINUX_SIZE" \
    --arg win_f "$(basename "$WIN_OUT")" --arg win_sha "$WIN_SHA" --argjson win_sz "$WIN_SIZE" \
    --arg commit "$COMMIT_SHA" --arg build_date "$BUILD_DATE_UTC" \
    '.commit = $commit | .build_date_utc = $build_date |
     .artifacts = ([.artifacts[] | select(.platform != "fuzzing-linux" and .platform != "fuzzing-windows")] +
       [
         {platform:"fuzzing-linux",file:$linux_f,sha256:$linux_sha,size_bytes:$linux_sz,kind:"cli"},
         {platform:"fuzzing-windows",file:$win_f,sha256:$win_sha,size_bytes:$win_sz,kind:"cli"}
       ])' "$MANIFEST" > "${MANIFEST}.new"
  mv "${MANIFEST}.new" "$MANIFEST"
else
  echo "[fuzzing-cli] WARN: no RELEASE_MANIFEST.json — skipping manifest patch" >&2
fi

echo "[fuzzing-cli] OK"
echo "  $LINUX_OUT ($(du -h "$LINUX_OUT" | awk '{print $1}'))"
echo "  $WIN_OUT ($(du -h "$WIN_OUT" | awk '{print $1}'))"

if [[ "$DEPLOY" == "1" ]]; then
  NODE_SSH="${NODE_SSH:-root@132.243.112.100}"
  RSYNC_SSH=(ssh)
  if [[ -n "${HACKME_DEPLOY_SSH_IDENTITY:-}" && -f "${HACKME_DEPLOY_SSH_IDENTITY}" ]]; then
    RSYNC_SSH=(ssh -i "${HACKME_DEPLOY_SSH_IDENTITY}" -o IdentitiesOnly=yes -o BatchMode=yes)
  elif [[ -f "${HOME}/.ssh/cursor_vps" ]]; then
    RSYNC_SSH=(ssh -i "${HOME}/.ssh/cursor_vps" -o IdentitiesOnly=yes -o BatchMode=yes)
  fi
  REMOTE="${NODE_DEPLOY_DIR:-/opt/hackme}/dist/release_${VERSION}/"
  echo "[fuzzing-cli] rsync → ${NODE_SSH}:${REMOTE}"
  rsync -e "${RSYNC_SSH[*]}" -avz \
    "$LINUX_OUT" "$WIN_OUT" "${DIST_DIR}/SHA256SUMS.txt" "${DIST_DIR}/RELEASE_MANIFEST.json" \
    "${NODE_SSH}:${REMOTE}"
  echo "[fuzzing-cli] deploy OK"
fi
