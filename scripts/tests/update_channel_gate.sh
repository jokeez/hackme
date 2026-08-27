#!/usr/bin/env bash
# Self-test L1 updater + latest.json schema against local dist (no network required).
#   bash scripts/tests/update_channel_gate.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_VERSION")}"
DIST="${DIST_DIR:-$ROOT/dist/release_${VERSION}}"
[[ -d "$DIST" ]] || { echo "[gate] missing $DIST — build or point DIST_DIR" >&2; exit 2; }
[[ -f "$DIST/hackme_${VERSION}_linux.tar.gz" ]] || { echo "[gate] missing linux tar" >&2; exit 2; }

bash "$ROOT/scripts/release/generate_latest_json.sh" "$DIST"
jq -e '.schema=="hackme.release.latest.v1"
  and (.version|length)>0
  and ([.platforms[]|select(.id=="linux")]|length)==1
  and (.platforms[]|select(.id=="linux")|.sha256|length)==64' \
  "$DIST/latest.json" >/dev/null
jq -e '.' "$ROOT/dist/latest.json" >/dev/null

# Rewrite URLs to file:// for offline test
LATEST="$DIST/latest.json"
LINUX_TAR="$DIST/hackme_${VERSION}_linux.tar.gz"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

jq --arg url "file://${LINUX_TAR}" --arg mirror "file://${LINUX_TAR}" \
  '(.platforms[] | select(.id=="linux") | .url) = $url
   | (.platforms[] | select(.id=="linux") | .mirror_url) = $mirror' \
  "$LATEST" >"$TMP/latest.json"

INSTALL="$TMP/install"
mkdir -p "$INSTALL/data" "$INSTALL/logs"
echo "SECRET_SHOULD_SURVIVE=1" >"$INSTALL/.env"
echo "old-binary" >"$INSTALL/hackme"
chmod +x "$INSTALL/hackme"
echo "version=0.0.0-old" >"$INSTALL/BUILD_INFO.txt"

echo "[gate] dry-run"
HACKME_LATEST_URL="file://${TMP}/latest.json" \
  bash "$ROOT/scripts/ops/update_hackme_miner.sh" --install-dir "$INSTALL" --dry-run --force

echo "[gate] real update"
HACKME_LATEST_URL="file://${TMP}/latest.json" \
  bash "$ROOT/scripts/ops/update_hackme_miner.sh" --install-dir "$INSTALL" --force

[[ -x "$INSTALL/hackme" ]] || { echo "[gate] FAIL hackme missing" >&2; exit 1; }
grep -q 'SECRET_SHOULD_SURVIVE=1' "$INSTALL/.env" || { echo "[gate] FAIL .env wiped" >&2; exit 1; }
[[ -d "$INSTALL/data" && -d "$INSTALL/logs" ]] || { echo "[gate] FAIL data/logs wiped" >&2; exit 1; }
grep -q "version=${VERSION}" "$INSTALL/BUILD_INFO.txt" || { echo "[gate] FAIL BUILD_INFO" >&2; exit 1; }
if grep -q 'old-binary' "$INSTALL/hackme" 2>/dev/null; then
  echo "[gate] FAIL hackme still stub" >&2
  exit 1
fi
[[ -d "$INSTALL/previous" ]] || { echo "[gate] FAIL no previous backup" >&2; exit 1; }

echo "[gate] idempotent (same version)"
HACKME_LATEST_URL="file://${TMP}/latest.json" \
  bash "$ROOT/scripts/ops/update_hackme_miner.sh" --install-dir "$INSTALL"

echo "[gate] bad sha rejected"
jq '(.platforms[]|select(.id=="linux")|.sha256)="0"*64' "$TMP/latest.json" >"$TMP/latest-bad.json"
# force different local so it attempts download
echo "version=0.0.0-force" >"$INSTALL/BUILD_INFO.txt"
set +e
HACKME_LATEST_URL="file://${TMP}/latest-bad.json" \
  bash "$ROOT/scripts/ops/update_hackme_miner.sh" --install-dir "$INSTALL" --force
bad_rc=$?
set -e
[[ "$bad_rc" -ne 0 ]] || { echo "[gate] FAIL expected SHA mismatch" >&2; exit 1; }
grep -q 'SECRET_SHOULD_SURVIVE=1' "$INSTALL/.env" || { echo "[gate] FAIL .env after bad sha" >&2; exit 1; }

echo "[gate] O1 wrapper"
mkdir -p "$TMP/opt/hackme"
echo "OS_SECRET=1" >"$TMP/opt/hackme/.env"
echo "version=old" >"$TMP/opt/hackme/BUILD_INFO.txt"
echo stub >"$TMP/opt/hackme/hackme"
chmod +x "$TMP/opt/hackme/hackme"
HACKME_LATEST_URL="file://${TMP}/latest.json" HACKME_INSTALL_DIR="$TMP/opt/hackme" \
  bash "$ROOT/scripts/ops/update_hackme_os_binaries.sh" --force
grep -q 'OS_SECRET=1' "$TMP/opt/hackme/.env"
[[ -x "$TMP/opt/hackme/hackme" ]]

echo "[gate] L2 deb staging (nfpm optional)"
# Skip full nfpm rebuild here if apt_deb_gate already ran; keep optional smoke.
if command -v nfpm >/dev/null 2>&1 && [[ "${UPDATE_GATE_BUILD_DEB:-0}" == "1" ]]; then
  bash "$ROOT/scripts/release/linux/build_deb_from_dist.sh" "$DIST" || true
fi

echo "[gate] PASS update channel L1+ (linux/O1/schema/sha) version=$VERSION"
