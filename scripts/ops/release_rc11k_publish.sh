#!/usr/bin/env bash
# rc11k launch candidate: gates → bundle → installer → ISO → deploy site+dist → VPS node.
#
#   bash scripts/ops/release_rc11k_publish.sh
#   SKIP_ISO=1 bash scripts/ops/release_rc11k_publish.sh
#   SKIP_GATES=1 SKIP_ISO=1 bash scripts/ops/release_rc11k_publish.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
VERSION="${VERSION:-0.1.0-rc11k}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
SKIP_ISO="${SKIP_ISO:-0}"
SKIP_INSTALLER="${SKIP_INSTALLER:-0}"
SKIP_GATES="${SKIP_GATES:-0}"
NEWS_ID="${NEWS_ID:-2026-06-03-rc11k-launch-candidate}"

echo "[rc11k] version=$VERSION commit=$(git rev-parse --short=12 HEAD 2>/dev/null || echo nogit)"

if [[ "$SKIP_GATES" != "1" ]]; then
  echo "[rc11k] go test (short, core)"
  go test -short -count=1 -timeout=120s ./internal/chain/ ./internal/store/ ./cmd/coordinator/ ./internal/hms/ .
  echo "[rc11k] prod probes"
  curl -fsS --max-time 15 "https://hackme.tech/api/status?lite=1" >/dev/null
  curl -fsS --max-time 25 "https://hackme.tech/api/status" >/dev/null
  STRESS_QUICK=1 bash scripts/tests/maximum_resilience_gate.sh
  bash scripts/tests/economics_confidence_gate.sh
  WORKER_SMOKE=0 bash scripts/ops/new_miner_journey_gate.sh
  BASE=https://hackme.tech bash scripts/tests/redteam_surface_smoke.sh
fi

echo "[rc11k] release bundle"
VERSION="$VERSION" bash scripts/ops/build_release_rc11k_bundle.sh

if [[ "$SKIP_INSTALLER" != "1" ]]; then
  echo "[rc11k] windows installer"
  VERSION="$VERSION" bash scripts/release/windows/build_installer.sh "$VERSION" || echo "[rc11k] WARN: installer build failed"
fi

if [[ "$SKIP_ISO" != "1" ]]; then
  echo "[rc11k] HackMe OS ISO (long)"
  VERSION="$VERSION" bash scripts/release/iso/build_hackme_miner_iso.sh
fi

VERSION="$VERSION" bash scripts/release/refresh_release_manifest.sh 2>/dev/null || true
ISO="dist/release_${VERSION}/HackMe-OS-${VERSION}-amd64.iso"
[[ -f "$ISO" ]] && (cd "dist/release_${VERSION}" && sha256sum "$(basename "$ISO")" > SHA256SUMS-iso.txt)

echo "[rc11k] deploy hackme-node (embed $VERSION)"
NODE_SSH="$NODE_SSH" DEPLOY_VERSION="$VERSION" bash scripts/ops/deploy_hackme_node.sh

echo "[rc11k] deploy site + dist"
NODE_SSH="$NODE_SSH" SKIP_DIST=0 bash scripts/ops/deploy_hackme_site.sh

echo "[rc11k] telegram (optional)"
FORCE_NEWS_ID="$NEWS_ID" NODE_SSH="$NODE_SSH" bash scripts/ops/publish_news_to_telegram.sh || echo "[rc11k] WARN: telegram skipped"

ISO_URL="https://hackme.tech/dist/release_${VERSION}/HackMe-OS-${VERSION}-amd64.iso" \
  bash scripts/tests/public_site_smoke.sh

echo "[rc11k] DONE — https://hackme.tech/downloads.html · verify commit in /api/status?lite=1"
