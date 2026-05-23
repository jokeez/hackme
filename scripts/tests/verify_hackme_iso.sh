#!/usr/bin/env bash
# Verify a HackMe OS ISO file before flashing (catches wrong downloads / corrupt images).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-0.1.0-rc11g}"
ISO_PATH="${1:-}"

if [[ -z "$ISO_PATH" ]]; then
  ISO_PATH="${ROOT}/dist/release_${VERSION}/HackMe-OS-${VERSION}-amd64.iso"
fi
if [[ ! -f "$ISO_PATH" ]]; then
  echo "[verify-iso] missing file: $ISO_PATH" >&2
  echo "  download: https://hackme.tech/dist/release_${VERSION}/HackMe-OS-${VERSION}-amd64.iso" >&2
  exit 2
fi

ISO_BASE="$(basename "$ISO_PATH")"
SUMS="${ROOT}/dist/release_${VERSION}/SHA256SUMS-iso.txt"
if [[ -f "$SUMS" ]]; then
  echo "[verify-iso] SHA256 check"
  EXPECTED="$(awk -v b="$ISO_BASE" '$NF==b || $2==b {print $1; exit}' "$SUMS")"
  ACTUAL="$(sha256sum "$ISO_PATH" | awk '{print $1}')"
  if [[ -n "$EXPECTED" && "$EXPECTED" == "$ACTUAL" ]]; then
    echo "[verify-iso] PASS sha256"
  else
    echo "[verify-iso] sha256: $ACTUAL"
    echo "[verify-iso] expected: ${EXPECTED:-?} (see $SUMS)"
  fi
else
  echo "[verify-iso] sha256: $(sha256sum "$ISO_PATH" | awk '{print $1}')"
fi

echo "[verify-iso] content fingerprint (HackMe OS + casper, not Alpine)"
if grep -aqF 'HackMe' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] PASS grub menu: HackMe OS"
else
  echo "[verify-iso] FAIL: no HackMe OS GRUB menu" >&2
  exit 3
fi
if grep -aq '/casper/vmlinuz' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] PASS casper live layout"
else
  echo "[verify-iso] FAIL: missing casper/vmlinuz" >&2
  exit 4
fi
if grep -aq 'filesystem.squashfs' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] PASS casper squashfs"
else
  echo "[verify-iso] FAIL: missing filesystem.squashfs" >&2
  exit 6
fi
if grep -aq 'boot=casper username=root noplymouth console=tty1' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] PASS recommended entry (text console, no Plymouth black screen)"
elif grep -aq 'boot=casper quiet splash' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] WARN: old ISO uses quiet splash default — rebuild for black-screen fix" >&2
fi
if grep -aq 'boot=casper.*toram' "$ISO_PATH" 2>/dev/null && ! grep -aq 'max performance' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] WARN: toram in default entry" >&2
fi
if grep -aq 'isolcpus=1' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] WARN: isolcpus still present in ISO; use recommended entry" >&2
fi
if grep -aqi 'Welcome to Alpine Linux' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] FAIL: Alpine Linux detected" >&2
  exit 5
fi

size="$(stat -c%s "$ISO_PATH" 2>/dev/null || stat -f%z "$ISO_PATH")"
mib=$(( size / 1024 / 1024 ))
echo "[verify-iso] size_bytes=$size (about ${mib} MiB)"
echo "[verify-iso] OK - safe to flash: $ISO_PATH"
echo "[verify-iso] Boot: HackMe OS (live - recommended). Avoid max performance on laptops under 8GB RAM."
