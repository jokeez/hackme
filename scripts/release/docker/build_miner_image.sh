#!/usr/bin/env bash
# Build hackme/miner:<VERSION> from a release dir (dist/release_<VERSION>) — the same tarball and
# SHA256SUMS users download, checked inside the build.
#   VERSION=0.1.0-rc19 bash scripts/release/docker/build_miner_image.sh [dist/release_<ver>]
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
VERSION="${VERSION:?set VERSION}"
DIST="${1:-$ROOT/dist/release_${VERSION}}"
TAR="$DIST/hackme_${VERSION}_linux.tar.gz"
[[ -f "$TAR" ]] || { echo "[docker] missing $TAR" >&2; exit 2; }
CTX="$(mktemp -d)"
trap 'rm -rf "$CTX"' EXIT
cp "$TAR" "$CTX/hackme_linux.tar.gz"
if [[ -f "$DIST/SHA256SUMS.txt" ]]; then
  grep -E "_linux\.tar\.gz$" "$DIST/SHA256SUMS.txt" >"$CTX/SHA256SUMS.txt"
else
  (cd "$DIST" && sha256sum "$(basename "$TAR")") >"$CTX/SHA256SUMS.txt"
  echo "[docker] WARN: no SHA256SUMS.txt in $DIST — pinned the local tarball hash" >&2
fi
cp "$ROOT/scripts/release/docker/Dockerfile.miner" "$ROOT/scripts/release/docker/miner-entrypoint.sh" "$CTX/"
IMAGE="${IMAGE:-hackme/miner}"
# DOCKER_BUILD_NETWORK=host when the default bridge has no DNS on the build machine.
docker build ${DOCKER_BUILD_NETWORK:+--network="$DOCKER_BUILD_NETWORK"} -f "$CTX/Dockerfile.miner" --build-arg HACKME_VERSION="$VERSION" \
  -t "$IMAGE:$VERSION" "$CTX"
echo "[docker] built $IMAGE:$VERSION"
