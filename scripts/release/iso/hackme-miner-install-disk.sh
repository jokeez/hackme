#!/usr/bin/env bash
# Install HackMe OS from live session to a local disk (persistent mining rig).
# Usage: sudo hackme-os-install /dev/sdX
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
case "$DISK" in
  /dev/sr* | /dev/cdrom | /dev/loop*) echo "Refusing install target $DISK (live media)" >&2; exit 1 ;;
esac

echo "WARNING: all data on ${DISK} will be destroyed in 5 seconds. Ctrl+C to abort."
sleep 5

ROOT_SRC="${HACKME_ROOT:-/opt/hackme}"
if [[ ! -d "$ROOT_SRC/bin" ]]; then
  echo "Missing ${ROOT_SRC}/bin — run from HackMe OS live session" >&2
  exit 1
fi

require_cmd() { command -v "$1" >/dev/null || { echo "missing: $1" >&2; exit 1; }; }
for c in parted mkfs.ext4 rsync grub-install update-grub blkid; do
  require_cmd "$c"
done

echo "[hackme-os-install] partition ${DISK}"
parted -s "$DISK" mklabel gpt
parted -s "$DISK" mkpart primary ext4 1MiB 100%
parted -s "$DISK" set 1 esp on 2>/dev/null || parted -s "$DISK" set 1 boot on
sleep 1
PART="${DISK}1"
[[ -b "$PART" ]] || PART="${DISK}p1"
[[ -b "$PART" ]] || { echo "partition not found: ${DISK}1" >&2; exit 1; }

echo "[hackme-os-install] mkfs.ext4 ${PART}"
mkfs.ext4 -F -L hackme-os "$PART"

MNT="$(mktemp -d)"
mount "$PART" "$MNT"

echo "[hackme-os-install] rsync live root → disk (this may take a few minutes)"
rsync -aHAX \
  --info=progress2 \
  --exclude='/proc/*' --exclude='/sys/*' --exclude='/dev/*' \
  --exclude='/run/*' --exclude='/tmp/*' --exclude='/mnt/*' --exclude='/media/*' \
  --exclude='/cdrom/*' --exclude='/isodevice/*' --exclude='/lib/live/mount/*' \
  --exclude='/cow' --exclude='/overlay' --exclude='/rofs' \
  / "$MNT/"

mkdir -p "$MNT/opt/hackme" "$MNT/var/lib/hackme" "$MNT/etc/hackme"
rsync -aHAX "${ROOT_SRC}/" "$MNT/opt/hackme/"
[[ -f /var/lib/hackme/miner.env ]] && cp -a /var/lib/hackme/miner.env "$MNT/var/lib/hackme/"
[[ -f /var/lib/hackme/hackme.ini ]] && cp -a /var/lib/hackme/hackme.ini "$MNT/var/lib/hackme/" 2>/dev/null || true
[[ -f /etc/hackme/pool.token ]] && cp -a /etc/hackme/pool.token "$MNT/etc/hackme/"

UUID="$(blkid -s UUID -o value "$PART")"
cat >"$MNT/etc/fstab" <<EOF
UUID=${UUID}  /  ext4  defaults,errors=remount-ro  0  1
EOF

# Installed system: drop casper live-only units, keep hackme miner stack.
for u in casper-md5check.service casper.service; do
  rm -f "$MNT/etc/systemd/system/${u}" \
    "$MNT/etc/systemd/system/multi-user.target.wants/${u}" 2>/dev/null || true
done

if [[ -f "$MNT/etc/default/grub" ]]; then
  sed -i 's/\(GRUB_CMDLINE_LINUX_DEFAULT=\)"[^"]*"/\1"noplymouth console=tty1"/' "$MNT/etc/default/grub" 2>/dev/null || true
  if ! grep -q '^GRUB_CMDLINE_LINUX_DEFAULT=' "$MNT/etc/default/grub" 2>/dev/null; then
    echo 'GRUB_CMDLINE_LINUX_DEFAULT="noplymouth console=tty1"' >>"$MNT/etc/default/grub"
  fi
fi

for d in dev proc sys run; do
  mount --bind "/$d" "$MNT/$d"
done

echo "[hackme-os-install] grub-install ${DISK}"
chroot "$MNT" grub-install "$DISK"
chroot "$MNT" update-grub 2>/dev/null || true

for d in run sys proc dev; do
  umount "$MNT/$d" 2>/dev/null || umount -l "$MNT/$d" 2>/dev/null || true
done
umount "$MNT"
rmdir "$MNT"

echo "[hackme-os-install] done — remove USB, boot from ${DISK}"
echo "[hackme-os-install] login: root (password in /etc/hackme/root-password; change with passwd; SSH keys only)"
