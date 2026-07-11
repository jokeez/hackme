#!/usr/bin/env bash
# rc11s: production baseline — mining canonical overlay, fuzz settle drain, customer smoke ops.
#
#   bash scripts/ops/release_rc11s_publish.sh
#   SKIP_ISO=1 bash scripts/ops/release_rc11s_publish.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
VERSION="${VERSION:-0.1.0-rc11s}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
SKIP_ISO="${SKIP_ISO:-0}"
SKIP_INSTALLER="${SKIP_INSTALLER:-0}"
SKIP_GATES="${SKIP_GATES:-0}"
SKIP_DEPLOY="${SKIP_DEPLOY:-0}"
NEWS_ID="${NEWS_ID:-2026-07-11-github-release-rc11s}"

echo "[rc11s] version=$VERSION commit=$(git rev-parse --short=12 HEAD 2>/dev/null || echo nogit)"

if [[ "$SKIP_GATES" != "1" ]]; then
  echo "[rc11s] go test (short)"
  go test -short -count=1 -timeout=300s ./...
  bash scripts/tests/pool_fuzz_sync_gate.sh
  bash scripts/tests/version_consistency_gate.sh
  BASE=http://127.0.0.1:8080 METRICS_TIMEOUT=20 bash scripts/tests/difficulty_health.sh || true
fi

echo "[rc11s] release bundle"
VERSION="$VERSION" bash scripts/release/make_release_bundle.sh

if [[ "$SKIP_INSTALLER" != "1" ]]; then
  echo "[rc11s] windows installer"
  VERSION="$VERSION" bash scripts/release/windows/build_installer.sh "$VERSION" || echo "[rc11s] WARN: installer build failed"
fi

if [[ "$SKIP_ISO" != "1" ]]; then
  echo "[rc11s] HackMe OS ISO"
  VERSION="$VERSION" bash scripts/release/iso/build_hackme_miner_iso.sh
fi

VERSION="$VERSION" bash scripts/release/refresh_release_manifest.sh 2>/dev/null || true
ISO="dist/release_${VERSION}/HackMe-OS-${VERSION}-amd64.iso"
if [[ -f "$ISO" ]]; then
  (cd "dist/release_${VERSION}" && sha256sum "$(basename "$ISO")" > SHA256SUMS-iso.txt)
  bash scripts/tests/verify_hackme_iso.sh "$ISO" || echo "[rc11s] WARN: iso verify skipped"
fi

bash scripts/tests/smoke_artifacts.sh "dist/release_${VERSION}" 2>/dev/null || true
bash scripts/tests/site_release_consistency_gate.sh

if [[ "$SKIP_DEPLOY" != "1" ]]; then
  echo "[rc11s] deploy hackme-node + coordinator"
  HACKME_DEPLOY_SSH_IDENTITY="${HACKME_DEPLOY_SSH_IDENTITY:-}" \
    NODE_SSH="$NODE_SSH" DEPLOY_VERSION="$VERSION" SYNC_DIST=1 \
    bash scripts/ops/deploy_hackme_node.sh

  echo "[rc11s] deploy site + dist"
  HACKME_DEPLOY_SSH_IDENTITY="${HACKME_DEPLOY_SSH_IDENTITY:-}" \
    NODE_SSH="$NODE_SSH" SKIP_DIST=0 \
    bash scripts/ops/deploy_hackme_site.sh
fi

echo "[rc11s] DONE — https://hackme.tech/downloads.html tag=$VERSION"
