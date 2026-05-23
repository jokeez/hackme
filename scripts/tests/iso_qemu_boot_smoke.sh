#!/usr/bin/env bash
# Boot HackMe OS ISO in QEMU (nographic) and check serial log for live session markers.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-0.1.0-rc11g}"
ISO="${1:-${ROOT}/dist/release_${VERSION}/HackMe-OS-${VERSION}-amd64.iso}"
LOG="${2:-/tmp/hackme-iso-qemu-$$.log}"
TIMEOUT_SEC="${TIMEOUT_SEC:-240}"

if [[ ! -f "$ISO" ]]; then
  echo "[iso-qemu] missing ISO: $ISO" >&2
  exit 2
fi
if ! command -v qemu-system-x86_64 >/dev/null 2>&1; then
  echo "[iso-qemu] SKIP: qemu-system-x86_64 not installed"
  exit 0
fi

rm -f "$LOG"
echo "[iso-qemu] booting ISO (GRUB default entry, ${TIMEOUT_SEC}s timeout)"
echo "[iso-qemu] log=$LOG"

# Full ISO boot — GRUB menu auto-starts default after timeout.
timeout "$TIMEOUT_SEC" qemu-system-x86_64 \
  -machine q35 \
  -m 4096 -smp 2 \
  -cdrom "$ISO" \
  -boot d \
  -nographic \
  -serial mon:stdio \
  -no-reboot \
  2>&1 | tee "$LOG" || true

pass=0
for needle in "casper" "systemd" "hackme" "HackMe" "login:" "root@"; do
  if grep -qi "$needle" "$LOG" 2>/dev/null; then
    echo "[iso-qemu] PASS log contains: $needle"
    pass=$((pass + 1))
  fi
done

if [[ "$pass" -lt 2 ]]; then
  echo "[iso-qemu] FAIL: boot log too empty — likely black-screen / early hang" >&2
  tail -n 50 "$LOG" 2>/dev/null || true
  exit 1
fi
echo "[iso-qemu] OK"
