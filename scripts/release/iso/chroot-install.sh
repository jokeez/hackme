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
  ca-certificates curl jq openssl \
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
  install -m 0755 /tmp/payload/minersign /opt/hackme/bin/minersign 2>/dev/null || true
  ln -sf workerpoh /opt/hackme/bin/workerpoh-cpu 2>/dev/null || true
  if [[ -f /tmp/payload/detect_gpu_backend.sh ]]; then
    install -m 0755 /tmp/payload/detect_gpu_backend.sh /opt/hackme/scripts/ops/detect_gpu_backend.sh
  fi
fi
if [[ -d /tmp/iso-scripts ]]; then
  cp -a /tmp/iso-scripts/. /opt/hackme/scripts/release/iso/
  chmod +x /opt/hackme/scripts/release/iso/*.sh
fi
if [[ -d /tmp/iso-overlay ]]; then
  cp -a /tmp/iso-overlay/etc/. /etc/
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

# Root access for rig operators (set password: passwd)
passwd -d root 2>/dev/null || true
echo 'root:hackme' | chpasswd 2>/dev/null || true

echo "hackme-os" >/etc/hostname
systemctl enable NetworkManager.service
systemctl enable hackme-os-tune.service
systemctl enable hackme-init-worker.service
systemctl enable hackme-zk-display.service
systemctl enable hackme-miner-firstboot.service
systemctl enable hackme-miner-worker.service
systemctl enable hackme-miner-status.service
systemctl enable ssh.service

if [[ -x /opt/hackme/scripts/release/iso/visual_overhaul.sh ]]; then
  echo "[chroot] visual overhaul (Plymouth + UI)"
  bash /opt/hackme/scripts/release/iso/visual_overhaul.sh chroot /
elif [[ -x /tmp/iso-scripts/visual_overhaul.sh ]]; then
  bash /tmp/iso-scripts/visual_overhaul.sh chroot /
fi

echo "[chroot] initramfs + kernel (live-boot + casper hooks)"
if [[ ! -x /usr/share/initramfs-tools/scripts/casper ]]; then
  echo "[chroot] WARN: casper initramfs hook missing — installing live-boot again" >&2
  apt-get install -y --no-install-recommends live-boot casper || true
fi
update-initramfs -c -k all 2>/dev/null || update-initramfs -u -k all
INITRD_CHECK="$(ls -1 /boot/initrd.img-* 2>/dev/null | sort -V | tail -1)"
if [[ -n "$INITRD_CHECK" ]] && command -v lsinitramfs >/dev/null 2>&1; then
  if lsinitramfs "$INITRD_CHECK" 2>/dev/null | grep -qE 'scripts/casper|scripts/casper-bottom'; then
    echo "[chroot] initrd includes casper live hook"
  else
    echo "[chroot] WARN: initrd may lack casper — check live-boot package" >&2
  fi
fi
apt-get clean
rm -rf /var/lib/apt/lists/*

echo "[chroot] done"
