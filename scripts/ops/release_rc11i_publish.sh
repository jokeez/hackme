#!/usr/bin/env bash
# Full rc11i publish: build artifacts, deploy VPS, telegram news.
#
#   bash scripts/ops/release_rc11i_publish.sh
#   SKIP_ISO=1 bash scripts/ops/release_rc11i_publish.sh   # faster (no ISO rebuild)
#   SKIP_INSTALLER=1 — skip Windows Inno Setup
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
VERSION="${VERSION:-0.1.0-rc11i}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
SKIP_ISO="${SKIP_ISO:-0}"
SKIP_INSTALLER="${SKIP_INSTALLER:-0}"
NEWS_ID="${NEWS_ID:-2026-05-26-fuzz-engine-v2-rc11i}"

echo "[rc11i] version=$VERSION"

echo "[rc11i] go test (short)"
go test ./... -short -count=1

echo "[rc11i] fuzz engine v2 gate (local)"
ADMIN="$(tr -d '\r\n' < .secrets/hackme_admin_token 2>/dev/null || true)"
if [[ -n "$ADMIN" ]]; then
  HACKME_ADMIN_TOKEN="$ADMIN" bash scripts/ops/fuzz_engine_v2_gate.sh
else
  echo "[rc11i] WARN: skip fuzz v2 gate (no admin token)"
fi

echo "[rc11i] release bundle"
VERSION="$VERSION" bash scripts/ops/build_release_rc11i_bundle.sh

if [[ "$SKIP_INSTALLER" != "1" ]]; then
  echo "[rc11i] windows installer (if Inno Setup available)"
  WIN_DIR="$ROOT/dist/release_${VERSION}/windows"
  if [[ -f "$WIN_DIR/hackme.iss" ]] && command -v iscc >/dev/null 2>&1; then
    (cd "$WIN_DIR" && iscc hackme.iss) || echo "[rc11i] WARN: iscc failed"
  else
    echo "[rc11i] skip installer (iscc not found)"
  fi
fi

if [[ "$SKIP_ISO" != "1" ]]; then
  echo "[rc11i] ISO build (long)"
  VERSION="$VERSION" bash scripts/release/iso/build_hackme_miner_iso.sh || echo "[rc11i] WARN: ISO build failed"
fi

echo "[rc11i] refresh manifest + checksums"
VERSION="$VERSION" bash scripts/release/refresh_release_manifest.sh 2>/dev/null || true

echo "[rc11i] deploy node binaries to VPS"
bash scripts/ops/apply_coordinator_perf_vps.sh

echo "[rc11i] rsync ops scripts"
rsync -az "$ROOT/scripts/ops/settle_worker_payouts.sh" \
  "$ROOT/scripts/ops/settlement_healthcheck.sh" \
  "$ROOT/scripts/ops/vps_host_sanity.sh" \
  "$ROOT/scripts/ops/fuzz_engine_v2_gate.sh" \
  "$ROOT/scripts/ops/exchange_listing_smoke.sh" \
  "$ROOT/scripts/ops/operator_phase1_gate.sh" \
  "$ROOT/scripts/ops/systemd/hackme-worker-settlement.service" \
  "${NODE_SSH}:/opt/hackme/scripts/ops/"

echo "[rc11i] deploy site + dist"
NODE_SSH="$NODE_SSH" SYNC_NGINX_SITE_CONF=0 bash scripts/ops/deploy_hackme_site.sh

echo "[rc11i] telegram news"
FORCE_NEWS_ID="$NEWS_ID" NODE_SSH="$NODE_SSH" bash scripts/ops/publish_news_to_telegram.sh || echo "[rc11i] WARN: telegram publish failed (SSH?)"

echo "[rc11i] production smoke"
PUBLIC_BASE=https://hackme.tech bash scripts/ops/mps_listing_readiness.sh --vps || true

echo "[rc11i] DONE — verify https://hackme.tech/downloads.html and https://hackme.tech/api/status version"
