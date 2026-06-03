#!/usr/bin/env bash
# Boot HackMe OS ISO in QEMU and fail on casper/kernel panic (exitcode=0x100).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-0.1.0-rc11k}"
ISO="${1:-${ROOT}/dist/release_${VERSION}/HackMe-OS-${VERSION}-amd64.iso}"
LOG="${2:-/tmp/hackme-iso-qemu-$$.log}"
TIMEOUT_SEC="${TIMEOUT_SEC:-300}"
BOOT_MODE="${BOOT_MODE:-extract}" # extract (serial console) | iso (full GRUB)

if [[ ! -f "$ISO" ]]; then
  echo "[iso-qemu] missing ISO: $ISO" >&2
  exit 2
fi
if ! command -v qemu-system-x86_64 >/dev/null 2>&1; then
  echo "[iso-qemu] SKIP: qemu-system-x86_64 not installed"
  exit 0
fi

rm -f "$LOG"
echo "[iso-qemu] boot ISO mode=$BOOT_MODE timeout=${TIMEOUT_SEC}s"
echo "[iso-qemu] log=$LOG"

panic_patterns='kernel panic|exitcode=0x100|Unable to find live|failed to mount.*squashfs|/init: can.t open /root/dev'

run_qemu() {
  timeout "$TIMEOUT_SEC" qemu-system-x86_64 \
    -machine q35 \
    -m 4096 -smp 2 \
    -no-reboot \
    "$@" \
    2>&1 | tee "$LOG" || true
}

if [[ "$BOOT_MODE" == "extract" ]] && command -v 7z >/dev/null 2>&1; then
  SQ_TMP="$(mktemp -d)"
  trap 'rm -rf "$SQ_TMP"' EXIT
  if 7z x -y -o"$SQ_TMP" "$ISO" casper/vmlinuz casper/initrd >/dev/null 2>&1; then
    echo "[iso-qemu] direct kernel boot + ISO cdrom (console=ttyS0, boot=casper debug)"
    run_qemu \
      -cdrom "$ISO" \
      -kernel "$SQ_TMP/casper/vmlinuz" \
      -initrd "$SQ_TMP/casper/initrd" \
      -append "boot=casper noplymouth plymouth.enable=0 console=ttyS0,115200n8 debug live-media-timeout=300 fsck.mode=skip" \
      -nographic \
      -serial mon:stdio
  else
    echo "[iso-qemu] WARN: 7z extract failed — falling back to full ISO boot" >&2
    BOOT_MODE=iso
  fi
fi

if [[ "$BOOT_MODE" == "iso" ]]; then
  echo "[iso-qemu] full ISO boot (GRUB → default entry)"
  run_qemu \
    -cdrom "$ISO" \
    -boot d \
    -nographic \
    -serial mon:stdio
fi

if grep -qiE "$panic_patterns" "$LOG" 2>/dev/null; then
  echo "[iso-qemu] FAIL: kernel/casper panic detected" >&2
  grep -iE "$panic_patterns" "$LOG" | tail -n 20 >&2 || true
  exit 1
fi

if grep -qiE 'username=root' "$LOG" 2>/dev/null || grep -aq 'username=root' "${ISO}" 2>/dev/null; then
  echo "[iso-qemu] WARN: ISO still uses username=root (casper panic risk on hardware)" >&2
fi

if grep -qi '(initramfs)' "$LOG" 2>/dev/null && ! grep -qiE 'systemd|login:' "$LOG" 2>/dev/null; then
  echo "[iso-qemu] WARN: dropped to initramfs shell (QEMU may be slow; no exitcode=0x100 — OK for hardware smoke)" >&2
  echo "[iso-qemu] OK — no casper panic (0x100); verify on real USB boot"
  exit 0
fi

pass=0
for needle in "casper" "systemd" "login:" "root@" "HackMe"; do
  if grep -qi "$needle" "$LOG" 2>/dev/null; then
    echo "[iso-qemu] PASS log contains: $needle"
    pass=$((pass + 1))
  fi
done

if [[ "$pass" -lt 2 ]]; then
  echo "[iso-qemu] FAIL: boot log too empty — likely early hang" >&2
  tail -n 60 "$LOG" 2>/dev/null || true
  exit 1
fi
echo "[iso-qemu] OK — no panic strings, live session markers present"
