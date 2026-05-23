#!/usr/bin/env bash
# HackMe OS boot tuning: CPU isolation mask, performance governor, IRQ affinity, drop-ins for worker RT.
set -uo pipefail

STATE_DIR="/run/hackme-os"
mkdir -p "$STATE_DIR"
LOG="${STATE_DIR}/tune.log"
exec > >(tee -a "$LOG") 2>&1

echo "[hackme-os] tune start $(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Live rig: no swap jitter (toram already in GRUB).
if command -v swapoff >/dev/null 2>&1; then
  swapoff -a 2>/dev/null || true
fi
for sw in /etc/fstab; do
  [[ -f "$sw" ]] && sed -i 's/^[[:space:]]*[^#].*[[:space:]]swap[[:space:]]/## swap disabled by hackme-os /' "$sw" 2>/dev/null || true
done

NCPU="$(nproc 2>/dev/null || echo 2)"
SYS_CPUS="0"
MINE_CPUS="0"
if [[ "$NCPU" -ge 8 ]]; then
  SYS_CPUS="0-1"
  MINE_CPUS="2-$((NCPU - 1))"
elif [[ "$NCPU" -ge 4 ]]; then
  SYS_CPUS="0"
  MINE_CPUS="1-$((NCPU - 1))"
else
  MINE_CPUS="0-$((NCPU - 1))"
fi
echo "$MINE_CPUS" >"${STATE_DIR}/worker-cpu-list"
echo "$SYS_CPUS" >"${STATE_DIR}/system-cpu-list"

# Persist for status / benchmark scripts
cat >"${STATE_DIR}/topology.json" <<EOF
{"ncpu":${NCPU},"system_cpus":"${SYS_CPUS}","miner_cpus":"${MINE_CPUS}"}
EOF

# cpufreq — performance on all cores
if command -v cpupower >/dev/null 2>&1; then
  cpupower frequency-set -g performance 2>/dev/null || true
elif [[ -d /sys/devices/system/cpu/cpu0/cpufreq ]]; then
  for gov in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
    echo performance >"$gov" 2>/dev/null || true
  done
fi

# Reduce jitter: disable irqbalance (set IRQ affinity to system CPUs)
if systemctl is-enabled irqbalance.service >/dev/null 2>&1; then
  systemctl stop irqbalance.service 2>/dev/null || true
  systemctl disable irqbalance.service 2>/dev/null || true
fi
if [[ -d /proc/irq && -n "$SYS_CPUS" ]]; then
  for irq in /proc/irq/*/smp_affinity_list; do
    echo "$SYS_CPUS" >"$irq" 2>/dev/null || true
  done
fi

# sysctl
sysctl --system >/dev/null 2>&1 || true

# Transparent hugepages — madvise (less latency spikes than always)
if [[ -f /sys/kernel/mm/transparent_hugepage/enabled ]]; then
  echo madvise >/sys/kernel/mm/transparent_hugepage/enabled 2>/dev/null || true
fi

# Stop desktop-ish services if present (live image / disk install)
for svc in bluetooth cups avahi-daemon ModemManager snapd whoopsie apport; do
  systemctl stop "${svc}.service" 2>/dev/null || true
  systemctl disable "${svc}.service" 2>/dev/null || true
done

# Worker systemd drop-in: FIFO scheduler + CPU affinity + max niceness
DROP_DIR="/etc/systemd/system/hackme-miner-worker.service.d"
mkdir -p "$DROP_DIR"
cat >"${DROP_DIR}/hackme-os.conf" <<EOF
[Service]
User=root
Group=root
CPUAffinity=${MINE_CPUS}
Nice=-20
CPUSchedulingPolicy=fifo
CPUSchedulingPriority=90
IOSchedulingClass=realtime
IOSchedulingPriority=0
LimitMEMLOCK=infinity
LimitRTPRIO=99
EOF
systemctl daemon-reload 2>/dev/null || true

# Root shell branding
cat >/etc/motd <<'MOTD'
╔══════════════════════════════════════════════════════════╗
║  HACKME OS — bare-metal kernel · neon pool rig           ║
║  GRUB/Plymouth themed · Zero-Knowledge wallet on USB   ║
╠══════════════════════════════════════════════════════════╣
║  hackme-os-status      GPU temp · fans · GH/s · pool     ║
║  hackme-show-wallet    HMC address + recovery phrase     ║
║  hackme-os-benchmark   60s local speed test              ║
║  hackme-os-install     persist to disk /dev/sdX          ║
║  journalctl -u hackme-miner-worker -f                    ║
╚══════════════════════════════════════════════════════════╝
MOTD

if [[ -x /opt/hackme/scripts/release/iso/hackme-os-gpu-tune.sh ]]; then
  /opt/hackme/scripts/release/iso/hackme-os-gpu-tune.sh || true
fi
if [[ -x /opt/hackme/scripts/release/iso/hackme-os-rig-profile.sh ]]; then
  /opt/hackme/scripts/release/iso/hackme-os-rig-profile.sh || true
fi

echo "[hackme-os] tune done ncpu=${NCPU} miner_cpus=${MINE_CPUS} system_cpus=${SYS_CPUS}"
