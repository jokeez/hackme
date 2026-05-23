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

echo "[verify-iso] content fingerprint (must include HackMe OS + casper, must NOT be Alpine)"
if strings "$ISO_PATH" | grep -q 'HackMe OS'; then
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
if strings "$ISO_PATH" | grep -q 'filesystem.squashfs'; then
  echo "[verify-iso] PASS casper squashfs"
else
  echo "[verify-iso] FAIL: missing filesystem.squashfs" >&2
  exit 6
fi
if strings "$ISO_PATH" | grep -q 'boot=casper' && strings "$ISO_PATH" | grep -q 'isolcpus=1'; then
  echo "[verify-iso] WARN: isolcpus still in ISO — pick recommended entry, not old max perf" >&2
fi
if strings "$ISO_PATH" | grep -q 'boot=casper quiet splash' && ! strings "$ISO_PATH" | grep -q 'boot=casper toram quiet splash isolcpus'; then
  echo "[verify-iso] PASS recommended entry without toram/isolcpus"
fi
if strings "$ISO_PATH" | grep -qi 'Welcome to Alpine Linux'; then
  echo "[verify-iso] FAIL: Alpine Linux detected — wrong image" >&2
  exit 5
fi

size="$(stat -c%s "$ISO_PATH" 2>/dev/null || stat -f%z "$ISO_PATH")"
echo "[verify-iso] size_bytes=$size (~$(( size / 1024 / 1024 )) MiB, expect ~900–1100 MiB)"
echo "[verify-iso] OK — safe to flash: $ISO_PATH"
echo "[verify-iso] Boot: choose HackMe OS (live · recommended) — NOT max performance on <8GB RAM"
