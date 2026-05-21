#!/usr/bin/env bash
# Install HackMe Miner rootfs to a local disk (persistent identity + logs).
# Usage: sudo hackme-miner-install-disk /dev/sdX
set -euo pipefail

if [[ "${EUID:-0}" -ne 0 ]]; then
  echo "Run as root: sudo $0 /dev/sdX" >&2
  exit 1
fi
if [[ $# -lt 1 ]]; then
  echo "Usage: sudo $0 /dev/sdX   (whole disk — will be erased)" >&2
  exit 1
fi

DISK="$1"
if [[ ! -b "$DISK" ]]; then
  echo "Not a block device: $DISK" >&2
  exit 1
fi

echo "WARNING: all data on ${DISK} will be destroyed in 5 seconds. Ctrl+C to abort."
sleep 5

ROOT_SRC="${HACKME_ROOT:-/opt/hackme}"
if [[ ! -d "$ROOT_SRC/bin" ]]; then
  echo "Missing ${ROOT_SRC}/bin — run from HackMe Miner live ISO" >&2
  exit 1
fi

require_cmd() { command -v "$1" >/dev/null || { echo "missing: $1" >&2; exit 1; }; }
require_cmd parted mkfs.ext4 rsync grub-install

parted -s "$DISK" mklabel gpt
parted -s "$DISK" mkpart primary ext4 1MiB 100%
parted -s "$DISK" set 1 boot on
sleep 1
PART="${DISK}1"
[[ -b "$PART" ]] || PART="${DISK}p1"

mkfs.ext4 -F -L hackme-miner "$PART"
MNT="$(mktemp -d)"
mount "$PART" "$MNT"

rsync -aHAX --exclude='/proc/*' --exclude='/sys/*' --exclude='/dev/*' --exclude='/run/*' --exclude='/tmp/*' \
  / "$MNT/" 2>/dev/null || rsync -aHAX /opt/hackme "$MNT/opt/" 

# Ensure full live root copied when possible
if [[ -d /opt/hackme/bin ]]; then
  mkdir -p "$MNT/opt/hackme"
  rsync -aHAX "${ROOT_SRC}/" "$MNT/opt/hackme/"
fi
mkdir -p "$MNT/var/lib/hackme" "$MNT/etc/hackme"
[[ -f /var/lib/hackme/miner.env ]] && cp -a /var/lib/hackme/miner.env "$MNT/var/lib/hackme/"
[[ -f /etc/hackme/pool.token ]] && cp -a /etc/hackme/pool.token "$MNT/etc/hackme/"

UUID="$(blkid -s UUID -o value "$PART")"
cat >"$MNT/etc/fstab" <<EOF
UUID=${UUID}  /  ext4  defaults  0 1
EOF

for d in dev proc sys run; do mount --bind "/$d" "$MNT/$d"; done
chroot "$MNT" grub-install "$DISK"
chroot "$MNT" update-grub 2>/dev/null || true
for d in dev proc sys run; do umount "$MNT/$d"; done
umount "$MNT"
rmdir "$MNT"

echo "[hackme-miner-install-disk] done — remove USB and boot from ${DISK}"
