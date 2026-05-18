#!/usr/bin/env bash
set -euo pipefail

# Generate Windows PE resources (icon + version metadata) for hackme.exe.
#
# Usage:
#   VERSION=1.0.0 bash scripts/release/windows/build_windows_resources.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "${ROOT_DIR}"

VERSION="${VERSION:-0.1.0-dev}"
ICON_PATH="${ICON_PATH:-scripts/release/windows/hackme.ico}"
TEMPLATE="${TEMPLATE:-scripts/release/windows/versioninfo.json.template}"
OUT_SYSO="${OUT_SYSO:-resource_windows_amd64.syso}"
OUT_JSON="${OUT_JSON:-scripts/release/windows/versioninfo.generated.json}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[res] missing command: $1" >&2
    exit 1
  }
}

need_cmd go
need_cmd jq

if [[ ! -f "${ICON_PATH}" ]]; then
  echo "[res] icon not found: ${ICON_PATH}" >&2
  exit 1
fi
if [[ ! -f "${TEMPLATE}" ]]; then
  echo "[res] template not found: ${TEMPLATE}" >&2
  exit 1
fi

if ! command -v goversioninfo >/dev/null 2>&1; then
  echo "[res] installing goversioninfo..."
  go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
  export PATH="${PATH}:$(go env GOPATH)/bin"
fi

icon_norm="${ICON_PATH//\\//}"
jq \
  --arg version "${VERSION}" \
  --arg icon "${icon_norm}" \
  '
  .StringFileInfo.FileVersion=$version
  | .StringFileInfo.ProductVersion=$version
  | .IconPath=$icon
  ' "${TEMPLATE}" > "${OUT_JSON}"

echo "[res] generating ${OUT_SYSO}"
goversioninfo -64 -o "${OUT_SYSO}" "${OUT_JSON}"
echo "[res] done: ${OUT_SYSO}"
