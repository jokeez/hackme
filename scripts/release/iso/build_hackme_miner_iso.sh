#!/usr/bin/env bash
# Build bootable HackMe Miner ISO (live USB → public pool worker).
#
# Usage:
#   VERSION=0.1.0-rc11l bash scripts/release/iso/build_hackme_miner_iso.sh
#
# Requires: Docker (recommended) or host debootstrap + squashfs-tools + xorriso + grub-mkrescue.
# Pool token: HACKME_RELEASE_POOL_MINER_TOKEN or .secrets/hackme_coordinator_worker_token
#
# Output:
#   dist/release_<VERSION>/HackMe-Miner-<VERSION>-amd64.iso

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT_DIR"

VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT_DIR}/scripts/release/CURRENT_ISO_VERSION" 2>/dev/null || echo 0.1.0-rc11l)}"
LINUX_TAR="${LINUX_TAR:-${ROOT_DIR}/dist/release_${VERSION}/hackme_${VERSION}_linux.tar.gz}"
OUT_DIR="${OUT_DIR:-${ROOT_DIR}/dist/release_${VERSION}}"
ISO_DIR="${ROOT_DIR}/scripts/release/iso"
STAGE="${STAGE:-${ROOT_DIR}/.cache/iso-stage-${VERSION}}"
USE_DOCKER="${USE_DOCKER:-1}"

if [[ -f "${ISO_DIR}/env.miner.iso" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "${ISO_DIR}/env.miner.iso"
  set +a
fi

POOL_TOKEN="${HACKME_RELEASE_POOL_MINER_TOKEN:-}"
if [[ -z "$POOL_TOKEN" && -f "${ROOT_DIR}/.secrets/hackme_coordinator_worker_token" ]]; then
  POOL_TOKEN="$(tr -d '\r\n' <"${ROOT_DIR}/.secrets/hackme_coordinator_worker_token")"
fi
if [[ -z "$POOL_TOKEN" ]]; then
  echo "[iso] WARN: no pool token — ISO will ship REPLACE_WITH_POOL_TOKEN (workers cannot claim)" >&2
  POOL_TOKEN="REPLACE_WITH_POOL_TOKEN"
fi

if [[ "${SKIP_RELEASE_BUILD:-0}" != "1" && ! -f "$LINUX_TAR" ]]; then
  echo "[iso] building linux release tarball first"
  VERSION="$VERSION" bash "${ROOT_DIR}/scripts/release/make_release_bundle.sh"
fi
if [[ ! -f "$LINUX_TAR" ]]; then
  echo "[iso] missing: $LINUX_TAR" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
rm -rf "$STAGE"
mkdir -p "$STAGE"
tar -xzf "$LINUX_TAR" -C "$STAGE"
PAYLOAD_DIR="$(find "$STAGE" -maxdepth 2 -type d -name linux | head -1)"
if [[ -z "$PAYLOAD_DIR" || ! -f "${PAYLOAD_DIR}/hackme" ]]; then
  echo "[iso] linux payload not found in ${LINUX_TAR}" >&2
  exit 1
fi
cp -f "${ROOT_DIR}/scripts/ops/detect_gpu_backend.sh" "${PAYLOAD_DIR}/detect_gpu_backend.sh"
chmod +x "${PAYLOAD_DIR}/detect_gpu_backend.sh" 2>/dev/null || true

chmod +x "${ISO_DIR}"/*.sh
if [[ -x "${ISO_DIR}/visual_overhaul.sh" ]]; then
  echo "[iso] visual overhaul module present (GRUB + Plymouth + TTY UI)"
fi

build_docker() {
  echo "[iso] Docker build (VERSION=${VERSION})"
  docker build -t hackme-miner-iso-build "${ISO_DIR}"
  docker run --rm --privileged --network host \
    -v "${PAYLOAD_DIR}:/payload:ro" \
    -v "${ISO_DIR}:/iso-scripts:ro" \
    -v "${ISO_DIR}/overlay:/iso-overlay:ro" \
    -v "${OUT_DIR}:/out" \
    -e VERSION="$VERSION" \
    -e POOL_TOKEN="$POOL_TOKEN" \
    -e UBUNTU_MIRROR="${UBUNTU_MIRROR:-http://us.archive.ubuntu.com/ubuntu/}" \
    -e PAYLOAD_DIR=/payload \
    -e ISO_SCRIPTS=/iso-scripts \
    -e ISO_OVERLAY=/iso-overlay \
    -e OUT_DIR=/out \
    hackme-miner-iso-build
  local rc=$?
  if [[ $rc -ne 0 ]]; then
    echo "[iso] FAIL: docker build exited $rc" >&2
    exit "$rc"
  fi
}

build_host() {
  echo "[iso] host build (VERSION=${VERSION})"
  export VERSION POOL_TOKEN
  export PAYLOAD_DIR
  export ISO_SCRIPTS="${ISO_DIR}"
  export ISO_OVERLAY="${ISO_DIR}/overlay"
  export OUT_DIR
  export WORK="${WORK:-${ROOT_DIR}/.cache/iso-work-${VERSION}}"
  bash "${ISO_DIR}/build_inner.sh"
}

mkdir -p "$OUT_DIR"
if [[ "$USE_DOCKER" == "1" ]] && command -v docker >/dev/null 2>&1; then
  build_docker
elif command -v debootstrap >/dev/null 2>&1; then
  build_host
else
  echo "[iso] install docker or debootstrap to build ISO" >&2
  echo "[iso]   sudo apt install docker.io   OR   sudo apt install debootstrap squashfs-tools xorriso mtools dosfstools grub-pc-bin" >&2
  exit 1
fi

echo "[iso] artifacts:"
ls -lh "${OUT_DIR}"/HackMe-OS-"${VERSION}"-amd64.iso "${OUT_DIR}"/SHA256SUMS-iso.txt 2>/dev/null || true
# Legacy name from older builds
ls -lh "${OUT_DIR}"/HackMe-Miner-"${VERSION}"-amd64.iso 2>/dev/null || true
