#!/usr/bin/env bash
# L2 — build hackme-node_<ver>_amd64.deb from an existing linux release tree / tar.
# Requires: nfpm (https://nfpm.goreleaser.com/) OR falls back to a staging tarball layout.
#
#   VERSION=0.1.0-rc15 bash scripts/release/linux/build_deb_from_dist.sh
#   bash scripts/release/linux/build_deb_from_dist.sh /path/to/dist/release_0.1.0-rc15
set -euo pipefail
# Package file modes must not depend on the builder's umask: a 002 umask made the rpm claim
# shared dirs (/usr/share/icons/hicolor…) as 0775 and conflict with hicolor-icon-theme.
umask 022
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
export PATH="${PATH}:$(go env GOPATH 2>/dev/null)/bin"
VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION" 2>/dev/null || true)}"
DIST_DIR="${1:-${ROOT}/dist/release_${VERSION}}"
[[ -d "$DIST_DIR" ]] || { echo "[deb] missing $DIST_DIR" >&2; exit 2; }
[[ -n "$VERSION" ]] || VERSION="$(basename "$DIST_DIR" | sed 's/^release_//')"

LINUX_TAR="${DIST_DIR}/hackme_${VERSION}_linux.tar.gz"
LINUX_DIR="${DIST_DIR}/linux"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

if [[ -d "$LINUX_DIR" && -x "${LINUX_DIR}/hackme" ]]; then
  SRC="$LINUX_DIR"
elif [[ -f "$LINUX_TAR" ]]; then
  mkdir -p "${STAGE}/extract"
  tar -xzf "$LINUX_TAR" -C "${STAGE}/extract"
  SRC="$(find "${STAGE}/extract" -maxdepth 2 -type d -name linux | head -1)"
  [[ -n "$SRC" && -x "${SRC}/hackme" ]] || { echo "[deb] linux/hackme not in tar" >&2; exit 1; }
else
  echo "[deb] need ${LINUX_DIR} or ${LINUX_TAR}" >&2
  exit 2
fi

# Staging package root under /opt/hackme (data/logs empty; never ship secrets / pool tokens)
PKG="${STAGE}/pkg"
mkdir -p "${PKG}/opt/hackme/data" "${PKG}/opt/hackme/logs" "${PKG}/usr/share/doc/hackme-node"
install -m 0755 "${SRC}/hackme" "${PKG}/opt/hackme/hackme"
for b in workerpoh workerfuzz minersign fleetplan workerpoh-opencl workerpoh-cuda hackme-wasm-check; do
  [[ -f "${SRC}/${b}" ]] && install -m 0755 "${SRC}/${b}" "${PKG}/opt/hackme/${b}"
done
[[ -d "${SRC}/bin" ]] && cp -a "${SRC}/bin" "${PKG}/opt/hackme/bin"
[[ -d "${SRC}/lib" ]] && cp -a "${SRC}/lib" "${PKG}/opt/hackme/lib"
for f in update_hackme_miner.sh update_hackme_os_binaries.sh start_hackme_miner.sh stop_hackme_miner.sh \
         start_hackme_orders.sh setup_hackme_miner.sh install_hackme.sh install_menu_entry.sh hackme_desktop_launch.sh \
         detect_gpu_backend.sh fix_miner_layout.sh \
         RELEASE_QUICKSTART.md README.md \
         hackme-node.service.template hackme.desktop.template hackme-dashboard.desktop.template \
         hackme-orders.desktop.template; do
  if [[ -f "${SRC}/${f}" ]]; then
    install -m 0755 "${SRC}/${f}" "${PKG}/opt/hackme/${f}" 2>/dev/null || \
      install -m 0644 "${SRC}/${f}" "${PKG}/opt/hackme/${f}"
  elif [[ -f "${ROOT}/scripts/release/linux/${f}" ]]; then
    install -m 0755 "${ROOT}/scripts/release/linux/${f}" "${PKG}/opt/hackme/${f}" 2>/dev/null || \
      install -m 0644 "${ROOT}/scripts/release/linux/${f}" "${PKG}/opt/hackme/${f}"
  elif [[ -f "${ROOT}/scripts/ops/${f}" ]]; then
    install -m 0755 "${ROOT}/scripts/ops/${f}" "${PKG}/opt/hackme/${f}"
  fi
done
# Worker ops scripts — required for POST /api/worker/start on Linux (also nested under scripts/ops/).
mkdir -p "${PKG}/opt/hackme/scripts/ops"
for op in worker_autostart.sh worker_loop.sh stop_pool_workers.sh resume_pool_mining.sh \
          purge_stale_pool_workers.sh detect_gpu_backend.sh desktop_worker_reset.sh; do
  src_op=""
  if [[ -f "${SRC}/scripts/ops/${op}" ]]; then
    src_op="${SRC}/scripts/ops/${op}"
  elif [[ -f "${SRC}/${op}" ]]; then
    src_op="${SRC}/${op}"
  elif [[ -f "${ROOT}/scripts/ops/${op}" ]]; then
    src_op="${ROOT}/scripts/ops/${op}"
  fi
  if [[ -n "$src_op" ]]; then
    install -m 0755 "$src_op" "${PKG}/opt/hackme/scripts/ops/${op}"
    # Flat copy next to hackme for resolveWorkerAutostartScript fallbacks.
    install -m 0755 "$src_op" "${PKG}/opt/hackme/${op}"
  fi
done
[[ -f "${PKG}/opt/hackme/update_hackme_miner.sh" ]] || \
  install -m 0755 "${ROOT}/scripts/ops/update_hackme_miner.sh" "${PKG}/opt/hackme/update_hackme_miner.sh"
[[ -f "${PKG}/opt/hackme/update_hackme_os_binaries.sh" ]] || \
  install -m 0755 "${ROOT}/scripts/ops/update_hackme_os_binaries.sh" "${PKG}/opt/hackme/update_hackme_os_binaries.sh"
[[ -f "${PKG}/opt/hackme/install_menu_entry.sh" ]] || \
  install -m 0755 "${ROOT}/scripts/release/linux/install_menu_entry.sh" "${PKG}/opt/hackme/install_menu_entry.sh"
[[ -f "${PKG}/opt/hackme/hackme_desktop_launch.sh" ]] || \
  install -m 0755 "${ROOT}/scripts/release/linux/hackme_desktop_launch.sh" "${PKG}/opt/hackme/hackme_desktop_launch.sh"
chmod 0755 "${PKG}/opt/hackme/hackme_desktop_launch.sh" 2>/dev/null || true
# Fail closed: deb without worker_autostart cannot mine.
if [[ ! -f "${PKG}/opt/hackme/worker_autostart.sh" && ! -f "${PKG}/opt/hackme/scripts/ops/worker_autostart.sh" ]]; then
  echo "[deb] ERROR: worker_autostart.sh missing from package staging" >&2
  exit 1
fi

# Icons (branded HackMe logo)
ICON_SRC="${SRC}/icons"
[[ -d "$ICON_SRC" ]] || ICON_SRC="${ROOT}/scripts/release/linux/icons"
mkdir -p "${PKG}/opt/hackme/icons" \
         "${PKG}/usr/share/icons/hicolor/48x48/apps" \
         "${PKG}/usr/share/icons/hicolor/128x128/apps" \
         "${PKG}/usr/share/icons/hicolor/256x256/apps" \
         "${PKG}/usr/share/applications"
if [[ -d "$ICON_SRC" ]]; then
  cp -a "${ICON_SRC}/." "${PKG}/opt/hackme/icons/"
  [[ -f "${ICON_SRC}/hackme-48.png" ]] && install -m 0644 "${ICON_SRC}/hackme-48.png" "${PKG}/usr/share/icons/hicolor/48x48/apps/hackme.png"
  [[ -f "${ICON_SRC}/hackme-128.png" ]] && install -m 0644 "${ICON_SRC}/hackme-128.png" "${PKG}/usr/share/icons/hicolor/128x128/apps/hackme.png"
  if [[ -f "${ICON_SRC}/hackme-256.png" ]]; then
    install -m 0644 "${ICON_SRC}/hackme-256.png" "${PKG}/usr/share/icons/hicolor/256x256/apps/hackme.png"
  elif [[ -f "${ICON_SRC}/hackme.png" ]]; then
    install -m 0644 "${ICON_SRC}/hackme.png" "${PKG}/usr/share/icons/hicolor/256x256/apps/hackme.png"
  fi
fi
# Desktop entries (fixed /opt/hackme paths for packaged install)
sed 's#__INSTALL_DIR__#/opt/hackme#g' \
  "${ROOT}/scripts/release/linux/hackme.desktop.template" \
  >"${PKG}/usr/share/applications/hackme.desktop"
sed 's#__INSTALL_DIR__#/opt/hackme#g' \
  "${ROOT}/scripts/release/linux/hackme-dashboard.desktop.template" \
  >"${PKG}/usr/share/applications/hackme-dashboard.desktop"
# Prefer desktop launcher; fall back only if it somehow missing
if [[ ! -f "${PKG}/opt/hackme/hackme_desktop_launch.sh" ]]; then
  if [[ -f "${PKG}/opt/hackme/start_hackme_miner.sh" ]]; then
    sed -i 's#/opt/hackme/hackme_desktop_launch.sh#/opt/hackme/start_hackme_miner.sh#g' \
      "${PKG}/usr/share/applications/hackme.desktop" \
      "${PKG}/usr/share/applications/hackme-dashboard.desktop" 2>/dev/null || true
  fi
fi
chmod 0644 "${PKG}/usr/share/applications/hackme.desktop" \
           "${PKG}/usr/share/applications/hackme-dashboard.desktop"

# Hard deny secrets in package
rm -f "${PKG}/opt/hackme/pool.miner.token" \
      "${PKG}/opt/hackme/.env" \
      "${PKG}/opt/hackme/.env.desktop" \
      "${PKG}/opt/hackme/.env.vps" \
      "${PKG}/opt/hackme/hackme.env" 2>/dev/null || true
find "${PKG}/opt/hackme" -name '*.seed' -delete 2>/dev/null || true

cat >"${PKG}/usr/share/doc/hackme-node/copyright" <<EOF
HackMe node package. See https://hackme.tech and LICENSE in the source tree.
EOF
cat >"${PKG}/usr/share/doc/hackme-node/README.Debian" <<EOF
hackme-node
===========

Binaries live in /opt/hackme. App menu launches use a per-user state dir
(~/.local/share/hackme + ~/.config/hackme) so icons work without root write
access under /opt/hackme.

  HackMe            — start node + open http://127.0.0.1:8080
  HackMe Dashboard  — same (open browser)

Optional pool mining: place pool.miner.token in ~/.config/hackme/ (from downloads).

CLI:

  bash /opt/hackme/hackme_desktop_launch.sh
  bash /opt/hackme/start_hackme_miner.sh   # tarball/writable install path

Self-update (L1, until apt L3):

  bash /opt/hackme/update_hackme_miner.sh
EOF

POSTINST="${STAGE}/postinst.sh"
cat >"$POSTINST" <<'EOF'
#!/bin/sh
set -e
mkdir -p /opt/hackme/data /opt/hackme/logs
# Desktop users (apt) must be able to flock worker locks under /opt/hackme/logs
# when an older worker_autostart ignores HACKME_WORKER_LOCK_DIR. Sticky + world-writable
# is intentional for multi-user miners on one box; preferred path is ~/.local/share/hackme/logs.
chmod 1777 /opt/hackme/logs 2>/dev/null || true
chmod 0755 /opt/hackme/hackme \
  /opt/hackme/update_hackme_miner.sh \
  /opt/hackme/start_hackme_miner.sh \
  /opt/hackme/stop_hackme_miner.sh \
  /opt/hackme/hackme_desktop_launch.sh \
  /opt/hackme/install_menu_entry.sh \
  /opt/hackme/worker_autostart.sh \
  /opt/hackme/resume_pool_mining.sh \
  /opt/hackme/scripts/ops/worker_autostart.sh \
  /opt/hackme/scripts/ops/resume_pool_mining.sh 2>/dev/null || true
# Refresh menu entries from packaged templates (idempotent)
if [ -x /opt/hackme/install_menu_entry.sh ]; then
  INSTALL_DIR=/opt/hackme PAYLOAD_DIR=/opt/hackme \
    /opt/hackme/install_menu_entry.sh >/dev/null 2>&1 || true
fi
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database /usr/share/applications >/dev/null 2>&1 || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -f /usr/share/icons/hicolor >/dev/null 2>&1 || true
fi
echo "hackme-node: menu HackMe / HackMe Dashboard → http://127.0.0.1:8080"
echo "hackme-node: user data in ~/.local/share/hackme (no root needed)"
echo "hackme-node: optional mining token → ~/.config/hackme/pool.miner.token"
echo "hackme-node: updates (L1): bash /opt/hackme/update_hackme_miner.sh"
EOF
chmod +x "$POSTINST"

chmod -R u+rwX,go-w,go+rX "$PKG"
# Relative paths for nfpm (run from STAGE)
NFPM_YAML="${STAGE}/nfpm.yaml"
cat >"$NFPM_YAML" <<EOF
name: hackme-node
arch: amd64
platform: linux
version: ${VERSION}
section: utils
priority: optional
maintainer: HackMe <ops@hackme.tech>
homepage: https://hackme.tech
license: Proprietary
description: |
  HackMe node + miner binaries. Data and env live under /opt/hackme.
  App menu entries with HackMe icon. Self-update via update_hackme_miner.sh (L1).
contents:
  - src: pkg/opt/hackme
    dst: /opt/hackme
    type: tree
  - src: pkg/usr/share/doc/hackme-node
    dst: /usr/share/doc/hackme-node
    type: tree
  - src: pkg/usr/share/applications
    dst: /usr/share/applications
    type: tree
  - src: pkg/usr/share/icons
    dst: /usr/share/icons
    type: tree
scripts:
  postinstall: postinst.sh
EOF

OUT_DEB="${DIST_DIR}/hackme-node_${VERSION}_amd64.deb"
# nfpm runs from STAGE — target must be absolute
OUT_DEB_ABS="$(cd "$(dirname "$OUT_DEB")" && pwd)/$(basename "$OUT_DEB")"
if command -v nfpm >/dev/null 2>&1; then
  (cd "$STAGE" && nfpm package --config nfpm.yaml --packager deb --target "$OUT_DEB_ABS")
  echo "[deb] wrote $OUT_DEB_ABS"
  ls -lh "$OUT_DEB_ABS"
  LIST="$(mktemp)"
  dpkg-deb -c "$OUT_DEB_ABS" >"$LIST"
  if grep -E 'pool\.miner\.token|/\.env$' "$LIST" >/dev/null; then
    rm -f "$LIST"
    echo "[deb] ERROR: secret-looking path inside package" >&2
    exit 1
  fi
  rm -f "$LIST"
  dpkg-deb -I "$OUT_DEB_ABS" | head -20 || true
  # rpm (Fedora/RHEL/openSUSE) + Arch from the same tree. Separate config with
  # version_schema: semver so 0.1.0-rc19 becomes 0.1.0~rc19 (pre-release sorts before the final
  # release). The .deb keeps its historical version string: changing it would break apt upgrades.
  EXTRA="${HACKME_EXTRA_PACKAGERS-rpm archlinux}"
  if [[ -n "$EXTRA" ]]; then
    { cat "$NFPM_YAML"; printf 'version_schema: semver\n'; } >"${STAGE}/nfpm.semver.yaml"
    # pacman drops a semver pre-release ("0.1.0-1" for every rc); its vercmp orders 0.1.0rc19 <
    # 0.1.0 natively, so Arch gets the version with the dash removed and no schema.
    sed 's/^version: .*/version: '"${VERSION//-/}"'/' "$NFPM_YAML" >"${STAGE}/nfpm.arch.yaml"
    for pk in $EXTRA; do
      case "$pk" in
        rpm) out="${DIST_DIR}/hackme-node-${VERSION}.x86_64.rpm" ;;
        archlinux) out="${DIST_DIR}/hackme-node-${VERSION}-x86_64.pkg.tar.zst" ;;
        *) echo "[pkg] unknown packager $pk" >&2; exit 1 ;;
      esac
      out_abs="$(cd "$(dirname "$out")" && pwd)/$(basename "$out")"
      cfg=nfpm.semver.yaml
      [[ "$pk" == archlinux ]] && cfg=nfpm.arch.yaml
      (cd "$STAGE" && nfpm package --config "$cfg" --packager "$pk" --target "$out_abs")
      echo "[pkg] wrote $out_abs"
    done
  fi
else
  FALLBACK="${DIST_DIR}/hackme-node_${VERSION}_amd64.deb.staging.tar.gz"
  tar -C "$PKG" -czf "$FALLBACK" .
  cp -f "$NFPM_YAML" "${DIST_DIR}/nfpm.hackme-node.yaml"
  echo "[deb] nfpm not installed — wrote staging $FALLBACK + nfpm.hackme-node.yaml" >&2
  exit 0
fi
