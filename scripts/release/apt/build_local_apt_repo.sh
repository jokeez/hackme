#!/usr/bin/env bash
# Build a local unsigned apt repo from hackme-node_*.deb under dist/.
# Does NOT publish or sign — for operator dry-runs before L3.
#
#   bash scripts/release/apt/build_local_apt_repo.sh
#   VERSION=0.1.0-rc15 bash scripts/release/apt/build_local_apt_repo.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION")}"
DIST="${DIST_DIR:-${ROOT}/dist/release_${VERSION}}"
DEB="${DEB_FILE:-${DIST}/hackme-node_${VERSION}_amd64.deb}"
REPO_ROOT="${APT_REPO_ROOT:-${ROOT}/dist/apt/repo}"
CODENAME="${APT_CODENAME:-unstable}"   # local dry-run suite name
COMPONENT="${APT_COMPONENT:-main}"
ARCH=amd64

[[ -f "$DEB" ]] || {
  echo "[apt] missing $DEB — run: bash scripts/release/linux/build_deb_from_dist.sh" >&2
  exit 2
}
for c in dpkg-scanpackages gzip; do
  command -v "$c" >/dev/null || { echo "[apt] missing $c (apt-utils / dpkg-dev)" >&2; exit 2; }
done

POOL="${REPO_ROOT}/pool/${COMPONENT}/h/hackme-node"
DIST_DIR="${REPO_ROOT}/dists/${CODENAME}/${COMPONENT}/binary-${ARCH}"
rm -rf "${REPO_ROOT}/pool" "${REPO_ROOT}/dists"
mkdir -p "$POOL" "$DIST_DIR"
cp -f "$DEB" "$POOL/"

(
  cd "$REPO_ROOT"
  dpkg-scanpackages "pool/${COMPONENT}" /dev/null >"dists/${CODENAME}/${COMPONENT}/binary-${ARCH}/Packages"
  gzip -9c "dists/${CODENAME}/${COMPONENT}/binary-${ARCH}/Packages" \
    >"dists/${CODENAME}/${COMPONENT}/binary-${ARCH}/Packages.gz"
)

# Unsigned Release (production MUST sign with gpg --clearsign / InRelease)
{
  echo "Origin: HackMe"
  echo "Label: HackMe"
  echo "Suite: ${CODENAME}"
  echo "Codename: ${CODENAME}"
  echo "Architectures: ${ARCH}"
  echo "Components: ${COMPONENT}"
  echo "Description: HackMe local unsigned apt repo (NOT for production)"
  echo "Date: $(date -Ru)"
  pkgs="dists/${CODENAME}/${COMPONENT}/binary-${ARCH}/Packages"
  pkggz="dists/${CODENAME}/${COMPONENT}/binary-${ARCH}/Packages.gz"
  echo "MD5Sum:"
  printf " %s %16d %s\n" "$(md5sum "${REPO_ROOT}/${pkgs}" | awk '{print $1}')" "$(stat -c%s "${REPO_ROOT}/${pkgs}")" "${COMPONENT}/binary-${ARCH}/Packages"
  printf " %s %16d %s\n" "$(md5sum "${REPO_ROOT}/${pkggz}" | awk '{print $1}')" "$(stat -c%s "${REPO_ROOT}/${pkggz}")" "${COMPONENT}/binary-${ARCH}/Packages.gz"
  echo "SHA256:"
  printf " %s %16d %s\n" "$(sha256sum "${REPO_ROOT}/${pkgs}" | awk '{print $1}')" "$(stat -c%s "${REPO_ROOT}/${pkgs}")" "${COMPONENT}/binary-${ARCH}/Packages"
  printf " %s %16d %s\n" "$(sha256sum "${REPO_ROOT}/${pkggz}" | awk '{print $1}')" "$(stat -c%s "${REPO_ROOT}/${pkggz}")" "${COMPONENT}/binary-${ARCH}/Packages.gz"
} >"${REPO_ROOT}/dists/${CODENAME}/Release"

# Example list file (file:// absolute)
LIST_OUT="${ROOT}/dist/apt/hackme-local.list"
ABS_REPO="$(cd "$REPO_ROOT" && pwd)"
cat >"$LIST_OUT" <<EOF
# LOCAL UNSIGNED — enable only for dry-run (apt will warn / need allow-insecure)
deb [trusted=yes] file:${ABS_REPO} ${CODENAME} ${COMPONENT}
EOF
cp -f "${ROOT}/scripts/release/apt/hackme.list.example" "${ROOT}/dist/apt/hackme.list.example" 2>/dev/null || true

echo "[apt] repo: $REPO_ROOT"
echo "[apt] list: $LIST_OUT"
echo "[apt] dry-run: sudo cp $LIST_OUT /etc/apt/sources.list.d/hackme-local.list && sudo apt update"
echo "[apt] NOTE: unsigned — production L3 requires GPG InRelease (see scripts/release/apt/README.md)"
