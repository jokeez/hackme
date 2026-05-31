#!/usr/bin/env bash
# Publish release artifacts (incl. HackMe OS ISO) + site to canonical VPS.
# Set VERSION (default rc11i). Wrapper: deploy_release_rc11i_iso.sh
#
# Usage:
#   NODE_SSH=hackme-vps NODE_DEPLOY_DIR=/opt/hackme bash scripts/ops/deploy_release_rc11g_iso.sh
#
# Prereq: passwordless SSH, dist/release_<VERSION>/HackMe-OS-*.iso built locally.

set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/ops/_deploy_ssh_retry.sh
source "$ROOT/scripts/ops/_deploy_ssh_retry.sh"

VERSION="${VERSION:-0.1.0-rc11i}"
NODE_SSH="${NODE_SSH:-}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
DIST="${ROOT}/dist/release_${VERSION}"
ISO="${DIST}/HackMe-OS-${VERSION}-amd64.iso"

if [[ -z "$NODE_SSH" ]]; then
  echo "[deploy-iso] set NODE_SSH (e.g. hackme-vps or root@host)" >&2
  exit 2
fi
if [[ ! -f "$ISO" ]]; then
  echo "[deploy-iso] missing ISO: $ISO" >&2
  echo "[deploy-iso] build: VERSION=$VERSION bash scripts/release/iso/build_hackme_miner_iso.sh" >&2
  exit 2
fi

echo "[deploy-iso] ISO $(du -h "$ISO" | awk '{print $1}') sha=$(awk '{print $1}' "${DIST}/SHA256SUMS-iso.txt" 2>/dev/null || echo '?')"

# Refresh manifest if ISO exists but manifest stale
if command -v jq >/dev/null 2>&1 && [[ -f "${DIST}/SHA256SUMS-iso.txt" ]]; then
  ISO_SHA="$(awk '{print $1}' "${DIST}/SHA256SUMS-iso.txt")"
  ISO_SIZE="$(stat -c%s "$ISO")"
  if [[ -f "${DIST}/RELEASE_MANIFEST.json" ]]; then
    jq --arg sha "$ISO_SHA" --argjson sz "$ISO_SIZE" --arg file "$(basename "$ISO")" \
      '.hackme_os_iso = true
       | .artifacts = ([.artifacts[] | select(.platform != "hackme-os")]
           + [{platform:"hackme-os",file:$file,sha256:$sha,size_bytes:$sz,kind:"iso",features:["zero_knowledge_start","live_usb"]}])' \
      "${DIST}/RELEASE_MANIFEST.json" > "${DIST}/RELEASE_MANIFEST.json.tmp" \
      && mv "${DIST}/RELEASE_MANIFEST.json.tmp" "${DIST}/RELEASE_MANIFEST.json"
    echo "[deploy-iso] patched RELEASE_MANIFEST.json (hackme_os_iso=true)"
  fi
fi

echo "[deploy-iso] rsync dist/ (large ISO — may take several minutes)"
deploy_ssh_retry_run rsync -az --progress \
  "${DIST}/" "${NODE_SSH}:${NODE_DEPLOY_DIR}/dist/release_${VERSION}/"

echo "[deploy-iso] rsync web/site + nginx reload"
NODE_SSH="$NODE_SSH" NODE_DEPLOY_DIR="$NODE_DEPLOY_DIR" SKIP_DIST=1 \
  bash "$ROOT/scripts/ops/deploy_hackme_site.sh"

echo "[deploy-iso] smoke downloads ISO HEAD"
code="$(curl -fsS --max-time 30 -o /dev/null -w '%{http_code}' \
  "https://hackme.tech/dist/release_${VERSION}/$(basename "$ISO")" || true)"
echo "[deploy-iso] https://hackme.tech/dist/.../$(basename "$ISO") → HTTP ${code:-error}"
[[ "$code" == "200" ]] || echo "[deploy-iso] WARN: expected HTTP 200 for ISO URL" >&2

echo "[deploy-iso] done — miners: https://hackme.tech/downloads.html#hackme-os"
