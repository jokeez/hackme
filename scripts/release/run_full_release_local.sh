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
# AMD/Intel OpenCL workerpoh-opencl needs CGO + ICD headers (default ON for full release).
export CGO_ENABLED="${CGO_ENABLED:-1}"
export BUILD_DEB="${BUILD_DEB:-1}"
export HACKME_RELEASE_POOL_MINER_TOKEN="${HACKME_RELEASE_POOL_MINER_TOKEN:-$(tr -d '\r\n' <.secrets/hackme_coordinator_worker_token 2>/dev/null || true)}"

# Release binaries must be reproducible from a published commit: rc19 shipped binaries built from
# an older commit plus local edits (vcs.modified=true) while the tag pointed at newer code.
# Override only for throwaway local builds: ALLOW_DIRTY_RELEASE=1.
if [[ "${ALLOW_DIRTY_RELEASE:-0}" != "1" ]]; then
  dirty="$(git status --porcelain --untracked-files=no 2>/dev/null || true)"
  if [[ -n "$dirty" ]]; then
    echo "[full-release] ERROR: uncommitted changes — commit or stash them first:" >&2
    echo "$dirty" | head -20 >&2
    exit 1
  fi
  git fetch -q origin main 2>/dev/null || echo "[full-release] WARN: could not fetch origin/main" >&2
  if ! git merge-base --is-ancestor HEAD origin/main 2>/dev/null; then
    echo "[full-release] ERROR: HEAD $(git rev-parse --short HEAD) is not on origin/main — push it first" >&2
    exit 1
  fi
  behind="$(git rev-list --count HEAD..origin/main 2>/dev/null || echo 0)"
  if [[ "$behind" != "0" ]]; then
    echo "[full-release] ERROR: HEAD is $behind commit(s) behind origin/main — git pull --rebase first" >&2
    exit 1
  fi
fi
echo "[full-release] VERSION=$VERSION CGO_ENABLED=$CGO_ENABLED commit=$(git rev-parse --short HEAD)"
bash scripts/release/apt/ensure_apt_signing_key.sh

echo "[full-release] 1/5 make_release_bundle"
BUILD_DEB=0 bash scripts/release/make_release_bundle.sh

echo "[full-release] 2/5 deb + signed apt"
bash scripts/release/linux/build_deb_from_dist.sh "dist/release_${VERSION}"
bash scripts/release/apt/publish_signed_apt_repo.sh
# Keep SHA256SUMS aware of the Linux packages (deb + rpm + Arch) when present.
(
  cd "dist/release_${VERSION}"
  if [[ -f SHA256SUMS.txt ]]; then
    for pkg in "hackme-node_${VERSION}_amd64.deb" "hackme-node-${VERSION}.x86_64.rpm" "hackme-node-${VERSION}-x86_64.pkg.tar.zst"; do
      [[ -f "$pkg" ]] || continue
      grep -v " ${pkg}\$" SHA256SUMS.txt >SHA256SUMS.txt.tmp || true
      sha256sum "$pkg" >>SHA256SUMS.txt.tmp
      mv SHA256SUMS.txt.tmp SHA256SUMS.txt
    done
  fi
)
# Optional miner image from the same tarball (BUILD_DOCKER=1; push is a separate, manual step).
if [[ "${BUILD_DOCKER:-0}" == "1" ]]; then
  VERSION="$VERSION" bash scripts/release/docker/build_miner_image.sh "dist/release_${VERSION}"
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
ls -lh "dist/release_${VERSION}"/*.{tar.gz,exe,zip,deb,rpm,zst,iso,json,txt} 2>/dev/null | head -40
# Published hashes must match the files (gates above no longer rebuild packages).
(cd "dist/release_${VERSION}" && sha256sum -c --ignore-missing SHA256SUMS.txt)
