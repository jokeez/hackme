#!/usr/bin/env bash
# Maximum local sandbox: unit gates + Docker apt install/upgrade dry-run.
# Does NOT publish and does NOT touch the host apt sources.
#
#   bash scripts/tests/update_apt_sandbox_gate.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export PATH="${PATH}:$(go env GOPATH 2>/dev/null)/bin"
VERSION="${VERSION:-$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_VERSION")}"
DIST="${DIST_DIR:-$ROOT/dist/release_${VERSION}}"
DEB="${DIST}/hackme-node_${VERSION}_amd64.deb"
REPO="${ROOT}/dist/apt/repo"
REPORT="${ROOT}/dist/apt/SANDBOX_REPORT.txt"
: >"$REPORT"
log() { echo "[sandbox] $*" | tee -a "$REPORT"; }

fail() { log "FAIL: $*"; exit 1; }

command -v docker >/dev/null || fail "docker required"
command -v jq >/dev/null || fail "jq required"
[[ -f "$DEB" ]] || {
  log "building deb…"
  bash "$ROOT/scripts/release/linux/build_deb_from_dist.sh" "$DIST"
}
[[ -d "$REPO/dists" ]] || bash "$ROOT/scripts/release/apt/build_local_apt_repo.sh"
[[ -f "$DEB" && -d "$REPO/pool" ]] || fail "missing deb/repo"

log "1/5 host gates"
if [[ "${SKIP_HOST_GATES:-0}" == "1" ]]; then
  log "skip host gates (SKIP_HOST_GATES=1)"
else
  bash "$ROOT/scripts/tests/update_channel_gate.sh" | tee -a "$REPORT"
  bash "$ROOT/scripts/tests/apt_deb_gate.sh" | tee -a "$REPORT"
  (cd "$ROOT" && go test -count=1 -run 'TestNormalizeReleaseVersion|TestUpdateAvailable|TestHandleUpdatesCheck' .) | tee -a "$REPORT"
fi

log "2/5 prepare offline latest.json for container"
OFFLINE="$(mktemp -d)"
trap 'rm -rf "$OFFLINE"' EXIT
# Point linux URL at the local tar inside the mounted dist (read-only)
LINUX_TAR="$DIST/hackme_${VERSION}_linux.tar.gz"
[[ -f "$LINUX_TAR" ]] || fail "missing linux tar"
jq --arg url "file:///dist-release/hackme_${VERSION}_linux.tar.gz" \
   --arg mirror "file:///dist-release/hackme_${VERSION}_linux.tar.gz" \
  '(.platforms[]|select(.id=="linux")|.url)=$url
   | (.platforms[]|select(.id=="linux")|.mirror_url)=$mirror' \
  "$DIST/latest.json" >"$OFFLINE/latest.json"

log "3/5 Docker apt install hackme-node"
IMG="${SANDBOX_IMG:-ubuntu:24.04}"
# Prefer host network so apt can resolve mirrors inside sandbox containers.
DOCKER_NET=(--network=host)
if [[ "${SANDBOX_NO_HOST_NET:-0}" == "1" ]]; then
  DOCKER_NET=()
fi
docker pull "$IMG" >/dev/null 2>&1 || true
# Mount repo + release dir; install via file:// apt
docker run --rm \
  "${DOCKER_NET[@]}" \
  -v "${REPO}:/apt-repo:ro" \
  -v "${DIST}:/dist-release:ro" \
  -v "${OFFLINE}:/offline:ro" \
  -e VERSION="$VERSION" \
  "$IMG" \
  bash -ec '
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
# Prefer our local unsigned repo; keep ubuntu for curl/jq if reachable
echo "deb [trusted=yes] file:/apt-repo unstable main" >/etc/apt/sources.list.d/hackme-local.list
apt-get update -qq || true
# Tools for L1 updater (best-effort if mirrors down)
apt-get install -y -qq ca-certificates curl jq 2>/dev/null || true
command -v curl >/dev/null || { echo "curl missing — install failed (network?)" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq missing" >&2; exit 2; }
apt-get update -qq
apt-get install -y -qq hackme-node
echo "[c] dpkg -l hackme-node:"
dpkg -l hackme-node | tail -1
test -x /opt/hackme/hackme
test -x /opt/hackme/update_hackme_miner.sh
# secrets must not appear from package
test ! -e /opt/hackme/pool.miner.token
test ! -e /opt/hackme/.env
# create fake env that must survive L1 update
echo "SANDBOX_SECRET=keep-me" >/opt/hackme/.env
chmod 600 /opt/hackme/.env
mkdir -p /opt/hackme/data /opt/hackme/logs
echo "version=0.0.0-sandbox" >/opt/hackme/BUILD_INFO.txt
# L1 dry-run against offline latest
HACKME_LATEST_URL=file:///offline/latest.json \
  bash /opt/hackme/update_hackme_miner.sh --install-dir /opt/hackme --dry-run --force
# Real L1 replace (force) — should keep .env; dpkg-owned needs --force
HACKME_LATEST_URL=file:///offline/latest.json \
  bash /opt/hackme/update_hackme_miner.sh --install-dir /opt/hackme --force
grep -q "SANDBOX_SECRET=keep-me" /opt/hackme/.env
test -x /opt/hackme/hackme
# Without --force: either up-to-date or soft-refuse dpkg
set +e
HACKME_LATEST_URL=file:///offline/latest.json \
  bash /opt/hackme/update_hackme_miner.sh --install-dir /opt/hackme >/tmp/refuse.out 2>&1
rc=$?
set -e
echo "[c] second update rc=$rc"
tail -5 /tmp/refuse.out
# apt reinstall — .env must survive (not a packaged conffile)
apt-get install -y -qq --reinstall hackme-node
grep -q "SANDBOX_SECRET=keep-me" /opt/hackme/.env
echo "[c] PASS docker apt install + L1 + reinstall"
' | tee -a "$REPORT"

log "4/5 direct dpkg -i in second throwaway container"
docker run --rm \
  "${DOCKER_NET[@]}" \
  -v "${DEB}:/pkg/hackme-node.deb:ro" \
  "$IMG" \
  bash -ec '
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
dpkg -i /pkg/hackme-node.deb
test -x /opt/hackme/hackme
test -x /opt/hackme/update_hackme_miner.sh
dpkg -L hackme-node | grep -q update_hackme_miner.sh
echo "[c] PASS dpkg -i"
' | tee -a "$REPORT"

log "5/5 summary"
{
  echo "version=$VERSION"
  echo "deb=$(basename "$DEB")"
  echo "deb_sha256=$(sha256sum "$DEB" | awk '{print $1}')"
  echo "platforms=$(jq -r '[.platforms[].id]|join(",")' "$DIST/latest.json")"
  echo "result=PASS"
  echo "note=unsigned local apt only; not published"
} | tee -a "$REPORT"

log "PASS — report: $REPORT"
