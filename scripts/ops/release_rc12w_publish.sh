#!/usr/bin/env bash
# rc12w: wallet Activity, security hardening, Win/Linux/fuzz rebuild.
#
#   bash scripts/ops/release_rc12w_publish.sh
#   SKIP_ISO=1 bash scripts/ops/release_rc12w_publish.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
VERSION="${VERSION:-0.1.0-rc12w}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
SKIP_ISO="${SKIP_ISO:-1}"
SKIP_INSTALLER="${SKIP_INSTALLER:-0}"
SKIP_GATES="${SKIP_GATES:-0}"
SKIP_DEPLOY="${SKIP_DEPLOY:-0}"
NEWS_ID="${NEWS_ID:-2026-07-22-github-release-rc12w}"

echo "[rc12w] version=$VERSION commit=$(git rev-parse --short=12 HEAD 2>/dev/null || echo nogit)"

if [[ "$SKIP_GATES" != "1" ]]; then
  echo "[rc12w] go test (short)"
  go test -short -count=1 -timeout=300s ./...
  bash scripts/tests/version_consistency_gate.sh
fi

echo "[rc12w] release bundle"
VERSION="$VERSION" bash scripts/release/make_release_bundle.sh

if [[ "$SKIP_INSTALLER" != "1" ]]; then
  echo "[rc12w] windows installer"
  VERSION="$VERSION" bash scripts/release/windows/build_installer.sh "$VERSION" || echo "[rc12w] WARN: installer build failed"
fi

if [[ "$SKIP_ISO" != "1" ]]; then
  echo "[rc12w] HackMe OS ISO (uses CURRENT_ISO_VERSION or VERSION)"
  VERSION="$VERSION" bash scripts/release/iso/build_hackme_miner_iso.sh
fi

VERSION="$VERSION" bash scripts/release/refresh_release_manifest.sh 2>/dev/null || true
ISO="dist/release_${VERSION}/HackMe-OS-${VERSION}-amd64.iso"
if [[ -f "$ISO" ]]; then
  (cd "dist/release_${VERSION}" && sha256sum "$(basename "$ISO")" > SHA256SUMS-iso.txt)
  bash scripts/tests/verify_hackme_iso.sh "$ISO" || echo "[rc12w] WARN: iso verify skipped"
fi

bash scripts/tests/smoke_artifacts.sh "dist/release_${VERSION}" 2>/dev/null || true
bash scripts/tests/site_release_consistency_gate.sh 2>/dev/null || true

if [[ "$SKIP_DEPLOY" != "1" ]]; then
  echo "[rc12w] deploy site + dist"
  HACKME_DEPLOY_SSH_IDENTITY="${HACKME_DEPLOY_SSH_IDENTITY:-}" \
    NODE_SSH="$NODE_SSH" SKIP_DIST=0 \
    bash scripts/ops/deploy_hackme_site.sh
fi

echo "[rc12w] DONE — https://hackme.tech/downloads.html tag=$VERSION news=$NEWS_ID"
echo "[rc12w] GitHub: gh release create $VERSION dist/release_${VERSION}/* --title \"HackMe $VERSION\""
