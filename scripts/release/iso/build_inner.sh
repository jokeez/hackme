#!/usr/bin/env bash
# Build HackMe Miner live ISO (run inside Docker or on host with debootstrap).
set -euo pipefail

VERSION="${VERSION:-0.1.0-dev}"
OUT_DIR="${OUT_DIR:-/out}"
PAYLOAD_DIR="${PAYLOAD_DIR:-/payload}"
ISO_SCRIPTS="${ISO_SCRIPTS:-/iso-scripts}"
ISO_OVERLAY="${ISO_OVERLAY:-/iso-overlay}"
POOL_TOKEN="${POOL_TOKEN:-REPLACE_WITH_POOL_TOKEN}"
UBUNTU_SUITE="${UBUNTU_SUITE:-noble}"
ARCH="${ARCH:-amd64}"
WORK="${WORK:-/work}"
CHROOT="${WORK}/chroot"
ISO_TREE="${WORK}/iso"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[iso-inner] missing: $1" >&2
    exit 1
  }
}
require_cmd debootstrap
require_cmd mksquashfs
require_cmd xorriso
require_cmd grub-mkrescue

mkdir -p "$OUT_DIR" "$WORK"
rm -rf "$CHROOT" "$ISO_TREE"
mkdir -p "$CHROOT" "$ISO_TREE"

echo "[iso-inner] debootstrap ${UBUNTU_SUITE} ${ARCH}"
debootstrap --arch="$ARCH" --variant=minbase "$UBUNTU_SUITE" "$CHROOT" "http://archive.ubuntu.com/ubuntu/"

mount --bind /dev "$CHROOT/dev"
mount --bind /dev/pts "$CHROOT/dev/pts"
mount -t proc proc "$CHROOT/proc"
mount -t sysfs sysfs "$CHROOT/sys"
trap 'umount -R "$CHROOT" 2>/dev/null || true' EXIT

mkdir -p "$CHROOT/tmp/payload" "$CHROOT/tmp/iso-scripts" "$CHROOT/tmp/iso-overlay"
if [[ -d "$PAYLOAD_DIR" ]]; then
  cp -a "$PAYLOAD_DIR"/. "$CHROOT/tmp/payload/"
fi
if [[ -d "$ISO_SCRIPTS" ]]; then
  cp -a "$ISO_SCRIPTS"/. "$CHROOT/tmp/iso-scripts/"
fi
if [[ -d "$ISO_OVERLAY" ]]; then
  cp -a "$ISO_OVERLAY"/. "$CHROOT/tmp/iso-overlay/"
fi
printf '%s' "$POOL_TOKEN" >"$CHROOT/tmp/pool.token"

cp "$(dirname "$0")/chroot-install.sh" "$CHROOT/tmp/chroot-install.sh"
chmod +x "$CHROOT/tmp/chroot-install.sh"
chroot "$CHROOT" bash /tmp/chroot-install.sh

KERNEL="$(ls -1 "$CHROOT"/boot/vmlinuz-* 2>/dev/null | sort -V | tail -1)"
INITRD="$(ls -1 "$CHROOT"/boot/initrd.img-* 2>/dev/null | sort -V | tail -1)"
if [[ -z "$KERNEL" || -z "$INITRD" ]]; then
  echo "[iso-inner] kernel/initrd not found in chroot" >&2
  exit 1
fi

echo "[iso-inner] squashfs"
mkdir -p "$ISO_TREE/casper"
mksquashfs "$CHROOT" "$ISO_TREE/casper/filesystem.squashfs" -comp zstd -Xcompression-level 6 -noappend
cp "$KERNEL" "$ISO_TREE/casper/vmlinuz"
cp "$INITRD" "$ISO_TREE/casper/initrd"
du -sh "$ISO_TREE/casper/filesystem.squashfs"

printf '%s\n' \
  "HackMe Miner ${VERSION}" \
  "Built $(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "Pool: https://hackme.tech/pool/coordinator" \
  >"$ISO_TREE/README.txt"

mkdir -p "$ISO_TREE/boot/grub"
cat >"$ISO_TREE/boot/grub/grub.cfg" <<'GRUB'
set default=0
set timeout=3
menuentry "HackMe Miner (live)" {
  linux /casper/vmlinuz boot=casper toram quiet splash ---
  initrd /casper/initrd
}
menuentry "HackMe Miner (live — safe graphics)" {
  linux /casper/vmlinuz boot=casper nomodeset toram quiet ---
  initrd /casper/initrd
}
GRUB

ISO_NAME="HackMe-Miner-${VERSION}-amd64.iso"
OUT_ISO="${OUT_DIR}/${ISO_NAME}"
rm -f "$OUT_ISO"

echo "[iso-inner] grub-mkrescue → ${OUT_ISO}"
grub-mkrescue -o "$OUT_ISO" "$ISO_TREE" -- \
  -volid "HACKME_OS_${VERSION}" 2>&1 | tail -5

sha256sum "$OUT_ISO" | tee "${OUT_DIR}/SHA256SUMS-iso.txt"
echo "[iso-inner] done: ${OUT_ISO} ($(du -h "$OUT_ISO" | awk '{print $1}'))"
