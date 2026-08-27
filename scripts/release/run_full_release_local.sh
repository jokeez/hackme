#!/usr/bin/env bash
# Full rc release pipeline (local): bundle → deb → signed apt → ISO → latest.json → gates.
# Does not push/deploy by itself — call after for gh/VPS.
#
#   VERSION=0.1.0-rc16 bash scripts/release/run_full_release_local.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export PATH="${PATH}:$(go env GOPATH 2>/dev/null)/bin"
VERSION="${VERSION:-$(tr -d ' \n\r' <scripts/release/CURRENT_VERSION)}"
export VERSION
export BUILD_DEB="${BUILD_DEB:-1}"
export HACKME_RELEASE_POOL_MINER_TOKEN="${HACKME_RELEASE_POOL_MINER_TOKEN:-$(tr -d '\r\n' <.secrets/hackme_coordinator_worker_token 2>/dev/null || true)}"

echo "[full-release] VERSION=$VERSION"
bash scripts/release/apt/ensure_apt_signing_key.sh

echo "[full-release] 1/5 make_release_bundle"
BUILD_DEB=0 bash scripts/release/make_release_bundle.sh

echo "[full-release] 2/5 deb + signed apt"
bash scripts/release/linux/build_deb_from_dist.sh "dist/release_${VERSION}"
bash scripts/release/apt/publish_signed_apt_repo.sh
# Also keep SHA256SUMS aware of deb if present
if [[ -f "dist/release_${VERSION}/hackme-node_${VERSION}_amd64.deb" ]]; then
  (
    cd "dist/release_${VERSION}"
    if [[ -f SHA256SUMS.txt ]]; then
      grep -v "hackme-node_${VERSION}_amd64.deb" SHA256SUMS.txt >SHA256SUMS.txt.tmp || true
      sha256sum "hackme-node_${VERSION}_amd64.deb" >>SHA256SUMS.txt.tmp
      mv SHA256SUMS.txt.tmp SHA256SUMS.txt
    fi
  )
fi

echo "[full-release] 3/5 ISO"
VERSION="$VERSION" bash scripts/release/iso/build_hackme_miner_iso.sh
bash scripts/release/refresh_release_manifest.sh "dist/release_${VERSION}" || true

echo "[full-release] 4/5 latest.json"
bash scripts/release/generate_latest_json.sh "dist/release_${VERSION}"

echo "[full-release] 5/5 gates"
bash scripts/tests/update_channel_gate.sh
bash scripts/tests/apt_deb_gate.sh || true
# Signed apt quick check
test -f dist/apt/repo/dists/stable/InRelease
gpg --verify dist/apt/repo/dists/stable/InRelease >/dev/null 2>&1 || \
  GNUPGHOME="$ROOT/.secrets/apt/gnupg" gpg --verify dist/apt/repo/dists/stable/InRelease

echo "[full-release] DONE dist/release_${VERSION}"
ls -lh "dist/release_${VERSION}"/*.{tar.gz,exe,zip,deb,iso,json,txt} 2>/dev/null | head -40
