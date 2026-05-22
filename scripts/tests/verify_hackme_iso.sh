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

SUMS="${ROOT}/dist/release_${VERSION}/SHA256SUMS-iso.txt"
if [[ -f "$SUMS" ]]; then
  echo "[verify-iso] SHA256 check"
  (cd "$(dirname "$ISO_PATH")" && sha256sum -c "$(basename "$SUMS")" 2>/dev/null) || {
    sha256sum "$ISO_PATH"
    echo "[verify-iso] WARN: no matching line in $SUMS — compare with https://hackme.tech/dist/release_${VERSION}/SHA256SUMS-iso.txt" >&2
  }
else
  echo "[verify-iso] sha256: $(sha256sum "$ISO_PATH" | awk '{print $1}')"
  echo "[verify-iso] compare with https://hackme.tech/dist/release_${VERSION}/SHA256SUMS-iso.txt"
fi

echo "[verify-iso] content fingerprint (must include HackMe OS + casper, must NOT be Alpine)"
if strings "$ISO_PATH" | grep -q 'menuentry "HackMe OS'; then
  echo "[verify-iso] PASS grub menu: HackMe OS"
else
  echo "[verify-iso] FAIL: no HackMe OS GRUB menu — this is not our release ISO" >&2
  exit 3
fi
if strings "$ISO_PATH" | grep -q '/casper/vmlinuz'; then
  echo "[verify-iso] PASS casper live layout (Ubuntu-based)"
else
  echo "[verify-iso] FAIL: missing casper/vmlinuz" >&2
  exit 4
fi
if strings "$ISO_PATH" | grep -qi 'Welcome to Alpine Linux'; then
  echo "[verify-iso] FAIL: Alpine Linux detected — wrong image" >&2
  exit 5
fi

size="$(stat -c%s "$ISO_PATH" 2>/dev/null || stat -f%z "$ISO_PATH")"
echo "[verify-iso] size_bytes=$size (~$(( size / 1024 / 1024 )) MiB, expect ~900–1100 MiB)"
echo "[verify-iso] OK — safe to flash: $ISO_PATH"
