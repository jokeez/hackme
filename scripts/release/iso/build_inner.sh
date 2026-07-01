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
require_cmd mformat

mkdir -p "$OUT_DIR" "$WORK"
rm -rf "$CHROOT" "$ISO_TREE"
mkdir -p "$CHROOT" "$ISO_TREE"

echo "[iso-inner] debootstrap ${UBUNTU_SUITE} ${ARCH}"
debootstrap --arch="$ARCH" --variant=minbase "$UBUNTU_SUITE" "$CHROOT" "http://archive.ubuntu.com/ubuntu/"

mount --bind /dev "$CHROOT/dev"
mount --bind /dev/pts "$CHROOT/dev/pts"
mount -t proc proc "$CHROOT/proc"
mount -t sysfs sysfs "$CHROOT/sys"
unmount_chroot_vfs() {
  umount "$CHROOT/proc" 2>/dev/null || umount -l "$CHROOT/proc" 2>/dev/null || true
  umount "$CHROOT/sys" 2>/dev/null || umount -l "$CHROOT/sys" 2>/dev/null || true
  umount "$CHROOT/dev/pts" 2>/dev/null || umount -l "$CHROOT/dev/pts" 2>/dev/null || true
  umount "$CHROOT/dev" 2>/dev/null || umount -l "$CHROOT/dev" 2>/dev/null || true
}
trap 'unmount_chroot_vfs' EXIT

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

CHROOT_INSTALL="${ISO_SCRIPTS}/chroot-install.sh"
if [[ ! -f "$CHROOT_INSTALL" ]]; then
  CHROOT_INSTALL="$(dirname "$0")/chroot-install.sh"
fi
if [[ ! -f "$CHROOT_INSTALL" ]]; then
  echo "[iso-inner] missing chroot-install.sh (ISO_SCRIPTS=$ISO_SCRIPTS)" >&2
  exit 1
fi
cp "$CHROOT_INSTALL" "$CHROOT/tmp/chroot-install.sh"
chmod +x "$CHROOT/tmp/chroot-install.sh"
chroot "$CHROOT" bash /tmp/chroot-install.sh

KERNEL="$(ls -1 "$CHROOT"/boot/vmlinuz-* 2>/dev/null | sort -V | tail -1)"
INITRD="$(ls -1 "$CHROOT"/boot/initrd.img-* 2>/dev/null | sort -V | tail -1)"
if [[ -z "$KERNEL" || -z "$INITRD" ]]; then
  echo "[iso-inner] kernel/initrd not found in chroot" >&2
  exit 1
fi

echo "[iso-inner] casper metadata (manifest)"
mkdir -p "$ISO_TREE/casper"
chroot "$CHROOT" dpkg-query -W --showformat='${Package} ${Version}\n' \
  >"$ISO_TREE/casper/filesystem.manifest" 2>/dev/null || true
cp -f "$ISO_TREE/casper/filesystem.manifest" "$ISO_TREE/casper/filesystem.manifest.du" 2>/dev/null || true

# Unmount bind mounts before squashfs (du with /proc mounted inflates filesystem.size → casper panic 0x100).
unmount_chroot_vfs

mkdir -p "${CHROOT}/dev" "${CHROOT}/proc" "${CHROOT}/sys" "${CHROOT}/run" "${CHROOT}/root" "${CHROOT}/tmp" "${CHROOT}/var/tmp"
chmod 700 "${CHROOT}/root"
find "${CHROOT}/tmp" "${CHROOT}/run" -mindepth 1 -delete 2>/dev/null || true

# Ubuntu casper initramfs reliably mounts xz squashfs.
echo "[iso-inner] squashfs (xz — casper-compatible)"
mksquashfs "$CHROOT" "$ISO_TREE/casper/filesystem.squashfs" \
  -comp xz -Xbcj x86 -b 1M -noappend \
  -e boot

# filesystem.size MUST match squashfs uncompressed bytes (casper rejects mismatches).
FS_SIZE=""
if command -v unsquashfs >/dev/null 2>&1; then
  FS_SIZE="$(unsquashfs -s "$ISO_TREE/casper/filesystem.squashfs" 2>/dev/null | awk '/^Filesystem size/ {print $3; exit}')"
fi
if [[ -z "$FS_SIZE" ]]; then
  FS_SIZE="$(du -sx --block-size=1 "$CHROOT" 2>/dev/null | cut -f1)"
fi
printf '%s\n' "$FS_SIZE" >"$ISO_TREE/casper/filesystem.size"
echo "[iso-inner] filesystem.size=${FS_SIZE} (must match squashfs uncompressed)"

DU_CHROOT="$(du -sx --block-size=1 "$CHROOT" 2>/dev/null | cut -f1)"
if [[ -n "$FS_SIZE" && -n "$DU_CHROOT" && "$FS_SIZE" != "$DU_CHROOT" ]]; then
  echo "[iso-inner] NOTE: post-squashfs size ${FS_SIZE} vs chroot du ${DU_CHROOT} (using squashfs size for casper)"
fi

cp "$KERNEL" "$ISO_TREE/casper/vmlinuz"
cp "$INITRD" "$ISO_TREE/casper/initrd"
du -sh "$ISO_TREE/casper/filesystem.squashfs"

if command -v lsinitramfs >/dev/null 2>&1; then
  IR_LIST="$(mktemp)"
  lsinitramfs "$INITRD" >"$IR_LIST" 2>/dev/null || true
  if grep -Eq 'scripts/casper|scripts/casper-bottom' "$IR_LIST" 2>/dev/null; then
    echo "[iso-inner] PASS initrd has casper hook"
  else
    echo "[iso-inner] FAIL initrd missing casper — ISO will not boot live" >&2
    exit 1
  fi
  if grep -Fq 'overlay.ko' "$IR_LIST" 2>/dev/null; then
    echo "[iso-inner] PASS initrd includes overlay.ko"
  else
    echo "[iso-inner] FAIL initrd missing overlay.ko — live boot will panic" >&2
    rm -f "$IR_LIST"
    exit 1
  fi
  if grep -Fq '05-hackme-overlay-modules' "$IR_LIST" 2>/dev/null; then
    echo "[iso-inner] PASS initrd includes 05-hackme-overlay-modules (casper-premount)"
  else
    echo "[iso-inner] FAIL initrd missing 05-hackme-overlay-modules" >&2
    rm -f "$IR_LIST"
    exit 1
  fi
  if grep -Fq 'hackme-kmods/overlay.ko' "$IR_LIST" 2>/dev/null; then
    echo "[iso-inner] PASS initrd includes uncompressed hackme-kmods/overlay.ko"
  else
    echo "[iso-inner] FAIL initrd missing hackme-kmods/overlay.ko" >&2
    rm -f "$IR_LIST"
    exit 1
  fi
  if grep -Fq 'hackme-kmods/zstd.ko' "$IR_LIST" 2>/dev/null; then
    echo "[iso-inner] PASS initrd includes hackme-kmods/zstd.ko"
  else
    echo "[iso-inner] WARN initrd missing hackme-kmods/zstd.ko" >&2
  fi
  if grep -Fq 'hackme-overlay-insmod' "$IR_LIST" 2>/dev/null; then
    echo "[iso-inner] PASS initrd casper patched (hackme-overlay-insmod)"
  else
    echo "[iso-inner] WARN initrd casper missing hackme-overlay-insmod patch" >&2
  fi
  rm -f "$IR_LIST"
fi

mkdir -p "$ISO_TREE/.disk"
printf '%s\n' "HackMe OS ${VERSION} (Ubuntu casper live)" >"$ISO_TREE/.disk/info"

printf '%s\n' \
  "HackMe Miner ${VERSION}" \
  "Built $(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "Pool: https://hackme.tech/pool/coordinator" \
  >"$ISO_TREE/README.txt"

if [[ -x "${ISO_SCRIPTS}/visual_overhaul.sh" ]]; then
  echo "[iso-inner] visual overhaul (GRUB theme)"
  bash "${ISO_SCRIPTS}/visual_overhaul.sh" iso-tree "$ISO_TREE"
else
  mkdir -p "$ISO_TREE/boot/grub"
  if [[ -f "${ISO_SCRIPTS}/grub-live.cfg" ]]; then
    cp -f "${ISO_SCRIPTS}/grub-live.cfg" "$ISO_TREE/boot/grub/grub.cfg"
  else
    cat >"$ISO_TREE/boot/grub/grub.cfg" <<'GRUB'
set default=0
set timeout=6
menuentry "HackMe OS (live · recommended)" {
  linux /casper/vmlinuz boot=casper noplymouth console=tty1 fsck.mode=skip ---
  initrd /casper/initrd
}
menuentry "HackMe OS (live · safe graphics)" {
  linux /casper/vmlinuz boot=casper nomodeset noplymouth console=tty1 fsck.mode=skip ---
  initrd /casper/initrd
}
GRUB
  fi
fi

ISO_NAME="HackMe-OS-${VERSION}-amd64.iso"
OUT_ISO="${OUT_DIR}/${ISO_NAME}"
rm -f "$OUT_ISO"

echo "[iso-inner] grub-mkrescue → ${OUT_ISO}"
grub-mkrescue -o "$OUT_ISO" "$ISO_TREE" -- \
  -volid "HACKME_OS_${VERSION}" 2>&1 | tail -5

if command -v isohybrid >/dev/null 2>&1; then
  echo "[iso-inner] isohybrid (USB whole-disk boot / GPT-friendly MBR)"
  isohybrid --uefi "$OUT_ISO" 2>/dev/null || isohybrid "$OUT_ISO" 2>/dev/null || true
fi

{
  sha256sum "$OUT_ISO" | awk -v f="$(basename "$OUT_ISO")" '{print $1"  "f}'
} >"${OUT_DIR}/SHA256SUMS-iso.txt"
chmod 644 "${OUT_DIR}/SHA256SUMS-iso.txt" 2>/dev/null || true
echo "[iso-inner] done: ${OUT_ISO} ($(du -h "$OUT_ISO" | awk '{print $1}'))"
