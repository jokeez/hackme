#!/usr/bin/env bash
# Build signed apt repo (stable) from hackme-node_*.deb for production publish.
#
#   bash scripts/release/apt/ensure_apt_signing_key.sh
#   VERSION=0.1.0-rc16 bash scripts/release/apt/publish_signed_apt_repo.sh
#
# Output: dist/apt/repo/ (pool + dists/stable + InRelease)
#         dist/apt/hackme.list
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
export PATH="${PATH}:$(go env GOPATH 2>/dev/null)/bin"
VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION")}"
DIST="${DIST_DIR:-${ROOT}/dist/release_${VERSION}}"
DEB="${DEB_FILE:-${DIST}/hackme-node_${VERSION}_amd64.deb}"
REPO_ROOT="${APT_REPO_ROOT:-${ROOT}/dist/apt/repo}"
CODENAME="${APT_CODENAME:-stable}"
COMPONENT=main
ARCH=amd64
SEC="${ROOT}/.secrets/apt"
export GNUPGHOME="${GNUPGHOME:-${SEC}/gnupg}"

[[ -f "$DEB" ]] || {
  echo "[apt-sign] missing $DEB — build deb first" >&2
  exit 2
}
[[ -d "$GNUPGHOME" ]] || bash "${ROOT}/scripts/release/apt/ensure_apt_signing_key.sh"
FPR="$(tr -d ' \n\r' <"${SEC}/fingerprint.txt" 2>/dev/null || true)"
[[ -n "$FPR" ]] || FPR="$(gpg --list-secret-keys --with-colons | awk -F: '/^fpr:/ {print $10; exit}')"
[[ -n "$FPR" ]] || { echo "[apt-sign] no signing key" >&2; exit 2; }

for c in dpkg-scanpackages gzip gpg; do
  command -v "$c" >/dev/null || { echo "[apt-sign] missing $c" >&2; exit 2; }
done

POOL="${REPO_ROOT}/pool/${COMPONENT}/h/hackme-node"
BIN_DIR="${REPO_ROOT}/dists/${CODENAME}/${COMPONENT}/binary-${ARCH}"
rm -rf "${REPO_ROOT}/pool" "${REPO_ROOT}/dists"
mkdir -p "$POOL" "$BIN_DIR"
cp -f "$DEB" "$POOL/"

(
  cd "$REPO_ROOT"
  dpkg-scanpackages "pool/${COMPONENT}" /dev/null >"dists/${CODENAME}/${COMPONENT}/binary-${ARCH}/Packages"
  gzip -9c "dists/${CODENAME}/${COMPONENT}/binary-${ARCH}/Packages" \
    >"dists/${CODENAME}/${COMPONENT}/binary-${ARCH}/Packages.gz"
)

{
  echo "Origin: HackMe"
  echo "Label: HackMe"
  echo "Suite: ${CODENAME}"
  echo "Codename: ${CODENAME}"
  echo "Architectures: ${ARCH}"
  echo "Components: ${COMPONENT}"
  echo "Description: HackMe node packages"
  echo "Date: $(date -Ru)"
  echo "Acquire-By-Hash: no"
  pkgs="dists/${CODENAME}/${COMPONENT}/binary-${ARCH}/Packages"
  pkggz="dists/${CODENAME}/${COMPONENT}/binary-${ARCH}/Packages.gz"
  echo "MD5Sum:"
  printf " %s %16d %s\n" "$(md5sum "${REPO_ROOT}/${pkgs}" | awk '{print $1}')" "$(stat -c%s "${REPO_ROOT}/${pkgs}")" "${COMPONENT}/binary-${ARCH}/Packages"
  printf " %s %16d %s\n" "$(md5sum "${REPO_ROOT}/${pkggz}" | awk '{print $1}')" "$(stat -c%s "${REPO_ROOT}/${pkggz}")" "${COMPONENT}/binary-${ARCH}/Packages.gz"
  echo "SHA256:"
  printf " %s %16d %s\n" "$(sha256sum "${REPO_ROOT}/${pkgs}" | awk '{print $1}')" "$(stat -c%s "${REPO_ROOT}/${pkgs}")" "${COMPONENT}/binary-${ARCH}/Packages"
  printf " %s %16d %s\n" "$(sha256sum "${REPO_ROOT}/${pkggz}" | awk '{print $1}')" "$(stat -c%s "${REPO_ROOT}/${pkggz}")" "${COMPONENT}/binary-${ARCH}/Packages.gz"
} >"${REPO_ROOT}/dists/${CODENAME}/Release"

# Detached + clearsigned InRelease
gpg --batch --yes --pinentry-mode loopback --passphrase '' \
  --default-key "$FPR" \
  --detach-sign --armor \
  -o "${REPO_ROOT}/dists/${CODENAME}/Release.gpg" \
  "${REPO_ROOT}/dists/${CODENAME}/Release"
gpg --batch --yes --pinentry-mode loopback --passphrase '' \
  --default-key "$FPR" \
  --clearsign \
  -o "${REPO_ROOT}/dists/${CODENAME}/InRelease" \
  "${REPO_ROOT}/dists/${CODENAME}/Release"

LIST_OUT="${ROOT}/dist/apt/hackme.list"
cat >"$LIST_OUT" <<EOF
deb [signed-by=/usr/share/keyrings/hackme-archive-keyring.gpg] https://hackme.tech/apt ${CODENAME} ${COMPONENT}
EOF
cp -f "$LIST_OUT" "${ROOT}/web/site/apt/hackme.list"
# Ensure public keyring on site
[[ -f "${ROOT}/web/site/apt/hackme-archive-keyring.gpg" ]] || \
  bash "${ROOT}/scripts/release/apt/ensure_apt_signing_key.sh"

# Nginx serves /apt/ from this repo root — bootstrap files must live here too.
for f in hackme-archive-keyring.gpg hackme-archive-keyring.asc hackme.list install.sh README.txt; do
  src="${ROOT}/web/site/apt/${f}"
  [[ -f "$src" ]] && cp -f "$src" "${REPO_ROOT}/${f}"
done
# Prefer generated list over site copy
cp -f "$LIST_OUT" "${REPO_ROOT}/hackme.list"

echo "[apt-sign] OK repo=${REPO_ROOT} suite=${CODENAME} fpr=${FPR:0:16}…"
echo "[apt-sign] list=${LIST_OUT}"
echo "[apt-sign] bootstrap: ${REPO_ROOT}/install.sh + keyring"
