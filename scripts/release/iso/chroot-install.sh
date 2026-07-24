#!/usr/bin/env bash
# Runs inside debootstrap chroot (or via chroot "$CHROOT" bash /tmp/chroot-install.sh).
set -euo pipefail

CHROOT="${CHROOT:-/}"
export DEBIAN_FRONTEND=noninteractive

# Skip service restarts during image build (dbus/NM not available in chroot).
cat >/usr/sbin/policy-rc.d <<'POL'
#!/bin/sh
exit 0
POL
chmod +x /usr/sbin/policy-rc.d

echo "[chroot] enable universe/multiverse for live + OpenCL"
if [[ -f /etc/apt/sources.list.d/ubuntu.sources ]]; then
  sed -i 's/Components: main$/Components: main universe restricted multiverse/' /etc/apt/sources.list.d/ubuntu.sources
elif [[ -f /etc/apt/sources.list ]]; then
  sed -i 's/ main$/ main universe restricted multiverse/' /etc/apt/sources.list 2>/dev/null || true
fi
if ! grep -q universe /etc/apt/sources.list /etc/apt/sources.list.d/* 2>/dev/null; then
  cat >>/etc/apt/sources.list <<'APT'
deb http://archive.ubuntu.com/ubuntu noble universe restricted multiverse
deb http://archive.ubuntu.com/ubuntu noble-updates universe restricted multiverse
APT
fi

echo "[chroot] apt packages"
apt-get update -y
apt-get install -y --no-install-recommends \
  systemd systemd-sysv dbus \
  linux-image-generic \
  live-boot live-boot-initramfs-tools live-config live-config-systemd \
  live-tools casper discover \
  network-manager openssh-server \
  ca-certificates curl jq openssl zstd sudo \
  ocl-icd-libopencl1 clinfo \
  grub-efi-amd64-bin \
  plymouth plymouth-label \
  lm-sensors \
  squashfs-tools rsync

echo "[chroot] hackme payload → /opt/hackme"
mkdir -p /opt/hackme/bin /opt/hackme/scripts/ops /opt/hackme/scripts/release/iso /opt/hackme/logs
if [[ -d /tmp/payload ]]; then
  install -m 0755 /tmp/payload/hackme /opt/hackme/bin/hackme 2>/dev/null || true
  install -m 0755 /tmp/payload/workerpoh /opt/hackme/bin/workerpoh 2>/dev/null || true
  install -m 0755 /tmp/payload/workerpoh-opencl /opt/hackme/bin/workerpoh-opencl 2>/dev/null || true
  install -m 0755 /tmp/payload/workerpoh-cuda /opt/hackme/bin/workerpoh-cuda 2>/dev/null || true
  install -m 0755 /tmp/payload/workerfuzz /opt/hackme/bin/workerfuzz 2>/dev/null || true
  install -m 0755 /tmp/payload/minersign /opt/hackme/bin/minersign 2>/dev/null || true
  # Also accept flat bin/ layout from release tarball.
  install -m 0755 /tmp/payload/bin/workerfuzz /opt/hackme/bin/workerfuzz 2>/dev/null || true
  ln -sf workerpoh /opt/hackme/bin/workerpoh-cpu 2>/dev/null || true
  if [[ -f /tmp/payload/detect_gpu_backend.sh ]]; then
    install -m 0755 /tmp/payload/detect_gpu_backend.sh /opt/hackme/scripts/ops/detect_gpu_backend.sh
  fi
  for op in worker_autostart.sh worker_loop.sh desktop_worker_reset.sh; do
    if [[ -f "/tmp/payload/scripts/ops/${op}" ]]; then
      install -m 0755 "/tmp/payload/scripts/ops/${op}" "/opt/hackme/scripts/ops/${op}"
    elif [[ -f "/tmp/payload/${op}" ]]; then
      install -m 0755 "/tmp/payload/${op}" "/opt/hackme/scripts/ops/${op}"
    fi
  done
fi
if [[ -d /tmp/iso-scripts ]]; then
  cp -a /tmp/iso-scripts/. /opt/hackme/scripts/release/iso/
  chmod +x /opt/hackme/scripts/release/iso/*.sh
fi
if [[ -d /tmp/iso-overlay ]]; then
  cp -a /tmp/iso-overlay/etc/. /etc/
fi

mkdir -p /etc/initramfs-tools/scripts/casper-premount /etc/initramfs-tools/scripts/local-premount
if [[ -f /tmp/iso-overlay/etc/initramfs-tools/scripts/casper-premount/05-hackme-overlay-modules ]]; then
  install -m 0755 /tmp/iso-overlay/etc/initramfs-tools/scripts/casper-premount/05-hackme-overlay-modules \
    /etc/initramfs-tools/scripts/casper-premount/05-hackme-overlay-modules
fi
if [[ -f /tmp/iso-overlay/etc/initramfs-tools/scripts/local-premount/00-hackme-overlay-modules ]]; then
  install -m 0755 /tmp/iso-overlay/etc/initramfs-tools/scripts/local-premount/00-hackme-overlay-modules \
    /etc/initramfs-tools/scripts/local-premount/00-hackme-overlay-modules
fi

mkdir -p /etc/hackme /var/lib/hackme
chmod 700 /etc/hackme /var/lib/hackme
if [[ -f /tmp/pool.token ]]; then
  install -m 0600 /tmp/pool.token /etc/hackme/pool.token
else
  echo "REPLACE_WITH_POOL_TOKEN" >/etc/hackme/pool.token
  chmod 0600 /etc/hackme/pool.token
fi

ln -sf /opt/hackme/scripts/release/iso/hackme-miner-status.sh /usr/local/bin/hackme-os-status 2>/dev/null || true
ln -sf /opt/hackme/scripts/release/iso/hackme-miner-status.sh /usr/local/bin/hackme-miner-status
ln -sf /opt/hackme/scripts/release/iso/hackme-miner-install-disk.sh /usr/local/bin/hackme-os-install
ln -sf /opt/hackme/scripts/release/iso/hackme-miner-install-disk.sh /usr/local/bin/hackme-miner-install-disk
ln -sf /opt/hackme/scripts/release/iso/hackme-miner-firstboot.sh /usr/local/bin/hackme-miner-firstboot
ln -sf /opt/hackme/scripts/release/iso/hackme-os-benchmark.sh /usr/local/bin/hackme-os-benchmark
ln -sf /opt/hackme/scripts/release/iso/hackme-os-tune.sh /usr/local/bin/hackme-os-tune
ln -sf /opt/hackme/scripts/release/iso/init-worker.sh /usr/local/bin/hackme-init-worker
ln -sf /opt/hackme/scripts/release/iso/hackme-show-wallet.sh /usr/local/bin/hackme-show-wallet
ln -sf /opt/hackme/scripts/release/iso/hackme-zk-display.sh /usr/local/bin/hackme-zk-display

# Root console password: random per image build (no hardcoded root:hackme).
# File is 0600; SSH is keys-only (see sshd_config.d). Change with: passwd
ROOT_PW="$(openssl rand -base64 32 2>/dev/null | tr -dc 'A-Za-z0-9' | head -c 24 || true)"
if [[ -z "$ROOT_PW" ]]; then
  ROOT_PW="$(head -c 48 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24)"
fi
echo "root:${ROOT_PW}" | chpasswd
printf '%s\n' "$ROOT_PW" >/etc/hackme/root-password
chmod 600 /etc/hackme/root-password
unset ROOT_PW

# Casper live-config expects ubuntu (25adduser also creates it — pre-seed for robustness).
# No NOPASSWD sudoers (C14): console/root uses /etc/hackme/root-password; SSH prefers keys.
if ! id ubuntu >/dev/null 2>&1; then
  getent group netdev >/dev/null 2>&1 || groupadd -r netdev 2>/dev/null || true
  useradd -m -s /bin/bash -G adm,cdrom,dip,plugdev,netdev ubuntu 2>/dev/null || \
    useradd -m -s /bin/bash -G adm,cdrom,dip,plugdev ubuntu
fi
passwd -l ubuntu 2>/dev/null || true
rm -f /etc/sudoers.d/99-hackme-live

echo "hackme-os" >/etc/hostname
systemctl enable hackme-boot-banner.service
systemctl enable getty@tty1.service
systemctl enable serial-getty@ttyS0.service 2>/dev/null || true
systemctl enable NetworkManager.service
systemctl enable hackme-os-tune.service
systemctl enable hackme-init-worker.service
systemctl enable hackme-zk-display.service
systemctl enable hackme-miner-firstboot.service
systemctl enable hackme-miner-worker.service
systemctl enable hackme-miner-status.service
systemctl enable ssh.service

# casper-md5check expects /cdrom/md5sum.txt (Ubuntu desktop ISO); we ship SHA256 on the website instead.
systemctl disable casper-md5check.service 2>/dev/null || true

if [[ -x /opt/hackme/scripts/release/iso/visual_overhaul.sh ]]; then
  echo "[chroot] visual overhaul (Plymouth + UI)"
  bash /opt/hackme/scripts/release/iso/visual_overhaul.sh chroot /
elif [[ -x /tmp/iso-scripts/visual_overhaul.sh ]]; then
  bash /tmp/iso-scripts/visual_overhaul.sh chroot /
fi

# Casper expects a root home in the live filesystem.
mkdir -p /root /run /var/lib/live
chmod 700 /root

if [[ -f /etc/initramfs-tools/initramfs.conf ]]; then
  sed -i 's/^MODULES=.*/MODULES=most/' /etc/initramfs-tools/initramfs.conf
fi

echo "[chroot] initramfs modules (squashfs + overlay — required for live boot)"
mkdir -p /etc/initramfs-tools
cat >/etc/initramfs-tools/modules <<'MOD'
squashfs
loop
zstd
xz
lzma2
lz4
overlay
iso9660
udf
vfat
ext4
sd_mod
usb_storage
MOD

chmod +x /etc/initramfs-tools/hooks/hackme-live-modules 2>/dev/null || true
if [[ -f /tmp/iso-overlay/etc/initramfs-tools/hooks/hackme-live-modules ]]; then
  install -m 0755 /tmp/iso-overlay/etc/initramfs-tools/hooks/hackme-live-modules \
    /etc/initramfs-tools/hooks/hackme-live-modules
fi

echo "[chroot] initramfs + kernel (live-boot + casper hooks)"
if ! ls /usr/share/initramfs-tools/hooks/*casper* >/dev/null 2>&1; then
  echo "[chroot] WARN: casper initramfs hook missing — installing live-boot again" >&2
  apt-get install -y --no-install-recommends live-boot casper || true
fi
update-initramfs -c -k all 2>/dev/null || update-initramfs -u -k all
# Initrd content checks run in build_inner.sh on the host (pipefail + chroot grep quirks).
apt-get clean
rm -rf /var/lib/apt/lists/*

echo "[chroot] done"
