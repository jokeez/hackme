#!/usr/bin/env bash
# rc11p: pool hashrate fix, metrics WAL, fuzz cleanup, security tests.
#
#   bash scripts/ops/release_rc11p_publish.sh
#   SKIP_ISO=1 bash scripts/ops/release_rc11p_publish.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
VERSION="${VERSION:-0.1.0-rc11p}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
SKIP_ISO="${SKIP_ISO:-0}"
SKIP_INSTALLER="${SKIP_INSTALLER:-0}"
SKIP_GATES="${SKIP_GATES:-0}"
NEWS_ID="${NEWS_ID:-2026-06-27-rc11p-pool-metrics-security}"

echo "[rc11p] version=$VERSION commit=$(git rev-parse --short=12 HEAD 2>/dev/null || echo nogit)"

if [[ "$SKIP_GATES" != "1" ]]; then
  echo "[rc11p] go test (short)"
  go test -short -count=1 -timeout=300s ./...
  bash scripts/tests/critical_security_pack.sh
  bash scripts/tests/security_assertions.sh
  bash scripts/tests/redteam_surface_smoke.sh
  BASE=http://127.0.0.1:8080 METRICS_TIMEOUT=15 bash scripts/tests/difficulty_health.sh
  bash scripts/tests/version_consistency_gate.sh
fi

echo "[rc11p] release bundle"
VERSION="$VERSION" bash scripts/release/make_release_bundle.sh

if [[ "$SKIP_INSTALLER" != "1" ]]; then
  echo "[rc11p] windows installer"
  VERSION="$VERSION" bash scripts/release/windows/build_installer.sh "$VERSION" || echo "[rc11p] WARN: installer build failed"
fi

if [[ "$SKIP_ISO" != "1" ]]; then
  echo "[rc11p] HackMe OS ISO"
  VERSION="$VERSION" bash scripts/release/iso/build_hackme_miner_iso.sh
fi

VERSION="$VERSION" bash scripts/release/refresh_release_manifest.sh 2>/dev/null || true
ISO="dist/release_${VERSION}/HackMe-OS-${VERSION}-amd64.iso"
if [[ -f "$ISO" ]]; then
  (cd "dist/release_${VERSION}" && sha256sum "$(basename "$ISO")" > SHA256SUMS-iso.txt)
  bash scripts/tests/verify_hackme_iso.sh "$ISO"
fi

bash scripts/tests/smoke_artifacts.sh "dist/release_${VERSION}" 2>/dev/null || true
bash scripts/tests/site_release_consistency_gate.sh

echo "[rc11p] deploy hackme-node + coordinator"
HACKME_DEPLOY_SSH_IDENTITY="${HACKME_DEPLOY_SSH_IDENTITY:-}" \
  NODE_SSH="$NODE_SSH" DEPLOY_VERSION="$VERSION" SYNC_DIST=1 \
  bash scripts/ops/deploy_hackme_node.sh

echo "[rc11p] deploy site + dist"
HACKME_DEPLOY_SSH_IDENTITY="${HACKME_DEPLOY_SSH_IDENTITY:-}" \
  NODE_SSH="$NODE_SSH" SKIP_DIST=0 \
  bash scripts/ops/deploy_hackme_site.sh

echo "[rc11p] live smoke"
SITE_BASE=https://hackme.tech NODE_SSH="$NODE_SSH" bash scripts/tests/public_site_smoke.sh || true

echo "[rc11p] telegram"
FORCE_NEWS_ID="$NEWS_ID" NODE_SSH="$NODE_SSH" \
  bash scripts/ops/publish_news_to_telegram.sh || echo "[rc11p] WARN: telegram skipped"

echo "[rc11p] DONE — https://hackme.tech/downloads.html"
