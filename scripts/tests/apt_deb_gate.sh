#!/usr/bin/env bash
# Local gates for L2 deb + L3 apt scaffold (no network publish).
#   bash scripts/tests/apt_deb_gate.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export PATH="${PATH}:$(go env GOPATH 2>/dev/null)/bin"
VERSION="${VERSION:-$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_VERSION")}"
DIST="${DIST_DIR:-$ROOT/dist/release_${VERSION}}"

command -v nfpm >/dev/null || { echo "[gate] nfpm required" >&2; exit 2; }
DEB="$DIST/hackme-node_${VERSION}_amd64.deb"
# Check the packages that were already built and hashed into SHA256SUMS / the apt pool:
# rebuilding here (nfpm is not reproducible) silently invalidated the published hashes.
if [[ ! -f "$DEB" || "${DEB_GATE_REBUILD:-0}" == "1" ]]; then
  bash "$ROOT/scripts/release/linux/build_deb_from_dist.sh" "$DIST"
fi
[[ -f "$DEB" ]] || { echo "[gate] missing deb" >&2; exit 1; }
LIST="$(mktemp)"
trap 'rm -f "$LIST"' EXIT
dpkg-deb -c "$DEB" >"$LIST"
if grep -E 'pool\.miner\.token|/\.env$' "$LIST" >/dev/null; then
  echo "[gate] FAIL secrets in deb" >&2
  exit 1
fi
grep -q 'update_hackme_miner.sh' "$LIST" || { echo "[gate] FAIL updater missing in deb" >&2; exit 1; }
grep -q 'usr/share/applications/hackme.desktop' "$LIST" || { echo "[gate] FAIL desktop entry missing" >&2; exit 1; }
grep -q 'hackme_desktop_launch.sh' "$LIST" || { echo "[gate] FAIL desktop launcher missing in deb" >&2; exit 1; }
grep -q 'usr/share/icons/hicolor/.*/apps/hackme.png' "$LIST" || { echo "[gate] FAIL icon missing" >&2; exit 1; }
# Desktop Exec must point at the XDG launcher (not bare xdg-open / unwritable start alone)
DESKTOP_TMP="$(mktemp -d)"
trap 'rm -f "$LIST"; rm -rf "$DESKTOP_TMP"' EXIT
dpkg-deb -x "$DEB" "$DESKTOP_TMP"
grep -q 'hackme_desktop_launch.sh' "$DESKTOP_TMP/usr/share/applications/hackme.desktop" || {
  echo "[gate] FAIL hackme.desktop Exec not desktop launcher" >&2
  exit 1
}
grep -q 'hackme_desktop_launch.sh' "$DESKTOP_TMP/usr/share/applications/hackme-dashboard.desktop" || {
  echo "[gate] FAIL dashboard.desktop Exec not desktop launcher" >&2
  exit 1
}
[[ -x "$DESKTOP_TMP/opt/hackme/hackme_desktop_launch.sh" ]] || {
  echo "[gate] FAIL launcher not executable in package" >&2
  exit 1
}

bash "$ROOT/scripts/release/apt/build_local_apt_repo.sh"
[[ -f "$ROOT/dist/apt/repo/dists/unstable/Release" ]] || { echo "[gate] FAIL apt Release" >&2; exit 1; }
[[ -f "$ROOT/dist/apt/hackme-local.list" ]] || { echo "[gate] FAIL list file" >&2; exit 1; }
grep -q 'hackme-node' "$ROOT/dist/apt/repo/dists/unstable/main/binary-amd64/Packages" || {
  echo "[gate] FAIL Packages missing hackme-node" >&2
  exit 1
}

# Refresh latest.json so linux_deb appears when .deb exists (local only)
bash "$ROOT/scripts/release/generate_latest_json.sh" "$DIST"
jq -e '([.platforms[]|select(.id=="linux_deb")]|length)==1' "$DIST/latest.json" >/dev/null || \
  echo "[gate] WARN: linux_deb not in latest.json (optional)"

echo "[gate] PASS apt/deb local scaffold version=$VERSION"
