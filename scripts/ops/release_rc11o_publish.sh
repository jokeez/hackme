#!/usr/bin/env bash
# rc11o production final: gates → bundle → installer → ISO → deploy → telegram.
#
#   bash scripts/ops/release_rc11o_publish.sh
#   SKIP_ISO=1 bash scripts/ops/release_rc11o_publish.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
VERSION="${VERSION:-0.1.0-rc11o}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
SKIP_ISO="${SKIP_ISO:-0}"
SKIP_INSTALLER="${SKIP_INSTALLER:-0}"
SKIP_GATES="${SKIP_GATES:-0}"
NEWS_ID="${NEWS_ID:-2026-06-16-rc11o-production-final}"

echo "[rc11o] version=$VERSION commit=$(git rev-parse --short=12 HEAD 2>/dev/null || echo nogit)"

if [[ "$SKIP_GATES" != "1" ]]; then
  echo "[rc11o] go test (short, core)"
  go test -short -count=1 -timeout=180s ./internal/chain/ ./internal/store/ ./cmd/coordinator/ ./internal/hms/ .
  bash scripts/tests/version_consistency_gate.sh
  bash scripts/release/build_listing_pdfs.sh
fi

echo "[rc11o] release bundle"
VERSION="$VERSION" bash scripts/release/make_release_bundle.sh

if [[ "$SKIP_INSTALLER" != "1" ]]; then
  echo "[rc11o] windows installer"
  VERSION="$VERSION" bash scripts/release/windows/build_installer.sh "$VERSION" || echo "[rc11o] WARN: installer build failed"
fi

if [[ "$SKIP_ISO" != "1" ]]; then
  echo "[rc11o] HackMe OS ISO (long)"
  VERSION="$VERSION" bash scripts/release/iso/build_hackme_miner_iso.sh
fi

VERSION="$VERSION" bash scripts/release/refresh_release_manifest.sh 2>/dev/null || true
ISO="dist/release_${VERSION}/HackMe-OS-${VERSION}-amd64.iso"
if [[ -f "$ISO" ]]; then
  (cd "dist/release_${VERSION}" && sha256sum "$(basename "$ISO")" > SHA256SUMS-iso.txt)
  bash scripts/tests/verify_hackme_iso.sh "$ISO"
  bash scripts/tests/iso_qemu_boot_smoke.sh "$ISO"
fi

bash scripts/tests/smoke_artifacts.sh "dist/release_${VERSION}"
bash scripts/tests/site_release_consistency_gate.sh

echo "[rc11o] deploy hackme-node"
HACKME_DEPLOY_SSH_IDENTITY="${HACKME_DEPLOY_SSH_IDENTITY:-}" \
  NODE_SSH="$NODE_SSH" DEPLOY_VERSION="$VERSION" SYNC_DIST=1 \
  bash scripts/ops/deploy_hackme_node.sh

echo "[rc11o] deploy site + dist"
HACKME_DEPLOY_SSH_IDENTITY="${HACKME_DEPLOY_SSH_IDENTITY:-}" \
  NODE_SSH="$NODE_SSH" SKIP_DIST=0 \
  bash scripts/ops/deploy_hackme_site.sh

echo "[rc11o] gates (live)"
SITE_BASE=https://hackme.tech NODE_SSH="$NODE_SSH" \
  bash scripts/tests/public_site_smoke.sh
SITE_BASE=https://hackme.tech bash scripts/tests/site_pages_audit.sh
NODE_SSH="$NODE_SSH" bash scripts/ops/run_miner_launch_gate.sh || echo "[rc11o] WARN: miner launch gate"

echo "[rc11o] telegram"
FORCE_NEWS_ID="$NEWS_ID" NODE_SSH="$NODE_SSH" \
  HACKME_DEPLOY_SSH_IDENTITY="${HACKME_DEPLOY_SSH_IDENTITY:-}" \
  bash scripts/ops/publish_news_to_telegram.sh || echo "[rc11o] WARN: telegram skipped"

echo "[rc11o] DONE — https://hackme.tech/downloads.html"
