#!/usr/bin/env bash
# Build HackMe-Setup-<version>.exe with Inno Setup 6.
#
# Usage (repo root):
#   bash scripts/release/windows/build_installer.sh [VERSION]
#   VERSION=0.1.0-rc11c bash scripts/release/make_release_bundle.sh  # builds installer if iscc/docker available

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "${ROOT_DIR}"

VERSION="${1:-${VERSION:-}}"
if [[ -z "${VERSION}" ]]; then
  echo "usage: $0 <version>   (or set VERSION=)" >&2
  exit 2
fi

DIST_DIR="${ROOT_DIR}/dist/release_${VERSION}"
WIN_DIR="${DIST_DIR}/windows"
ISS="${ROOT_DIR}/scripts/release/windows/hackme.iss"

if [[ ! -f "${WIN_DIR}/hackme.exe" ]]; then
  echo "[inno] missing ${WIN_DIR}/hackme.exe — run: VERSION=${VERSION} bash scripts/release/make_release_bundle.sh" >&2
  exit 1
fi
if [[ ! -f "${WIN_DIR}/pool.miner.token" ]]; then
  echo "[inno] missing ${WIN_DIR}/pool.miner.token — rebuild release with .secrets/hackme_coordinator_worker_token" >&2
  exit 1
fi
if [[ ! -f "${ISS}" ]]; then
  echo "[inno] missing ${ISS}" >&2
  exit 1
fi

run_iscc() {
  local iscc_bin="$1"
  echo "[inno] ${iscc_bin} /DMyAppVersion=${VERSION} ${ISS}"
  "${iscc_bin}" "/DMyAppVersion=${VERSION}" "/DMyAppPublisher=HackMe Network" "${ISS}"
}

built=0
if command -v iscc >/dev/null 2>&1; then
  run_iscc iscc
  built=1
elif [[ -x "/usr/bin/iscc" ]]; then
  run_iscc /usr/bin/iscc
  built=1
elif command -v docker >/dev/null 2>&1; then
  echo "[inno] trying Docker image amake/innosetup ..."
  docker run --rm -v "${ROOT_DIR}:/work" -w /work amake/innosetup \
    "/DMyAppVersion=${VERSION}" "/DMyAppPublisher=HackMe Network" "scripts/release/windows/hackme.iss"
  built=1
fi

if [[ "${built}" -eq 0 ]]; then
  echo "[inno] SKIP: install Inno Setup 6 (iscc in PATH) or Docker, then re-run:" >&2
  echo "       bash scripts/release/windows/build_installer.sh ${VERSION}" >&2
  exit 0
fi

OUT="${DIST_DIR}/HackMe-Setup-${VERSION}.exe"
ALT="${ROOT_DIR}/scripts/release/windows/dist/release_${VERSION}/HackMe-Setup-${VERSION}.exe"
if [[ ! -f "${OUT}" && -f "${ALT}" ]]; then
  mv -f "${ALT}" "${OUT}"
fi
if [[ ! -f "${OUT}" ]]; then
  echo "[inno] expected output missing: ${OUT}" >&2
  exit 1
fi

SIZE="$(stat -c%s "${OUT}")"
echo "[inno] OK: ${OUT} (${SIZE} bytes)"
