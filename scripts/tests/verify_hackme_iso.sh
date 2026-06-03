#!/usr/bin/env bash
# Verify a HackMe OS ISO file before flashing (catches wrong downloads / corrupt images).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-0.1.0-rc11k}"
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
    echo "[verify-iso] FAIL sha256: $ACTUAL" >&2
    echo "[verify-iso] expected: ${EXPECTED:-?} (see $SUMS)" >&2
    exit 8
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
if command -v 7z >/dev/null 2>&1 && command -v unsquashfs >/dev/null 2>&1; then
  SQ_TMP="$(mktemp -d)"
  FSZ_TMP="$(mktemp)"
  if 7z x -y -o"$SQ_TMP" "$ISO_PATH" casper/filesystem.squashfs casper/filesystem.size >/dev/null 2>&1; then
    SQ_UNC="$(unsquashfs -s "$SQ_TMP/casper/filesystem.squashfs" 2>/dev/null | awk '/^Filesystem size/ {print $3; exit}')"
    SQ_FILE="$(tr -d '\r\n' <"$SQ_TMP/casper/filesystem.size" 2>/dev/null || true)"
    if [[ -n "$SQ_UNC" && -n "$SQ_FILE" && "$SQ_UNC" == "$SQ_FILE" ]]; then
      echo "[verify-iso] PASS filesystem.size matches squashfs ($SQ_UNC bytes)"
    else
      echo "[verify-iso] FAIL filesystem.size=$SQ_FILE squashfs=$SQ_UNC (casper will panic)" >&2
      exit 7
    fi
    if unsquashfs -s "$SQ_TMP/casper/filesystem.squashfs" 2>/dev/null | grep -qi 'Compression.*xz'; then
      echo "[verify-iso] PASS squashfs compression xz"
    elif unsquashfs -s "$SQ_TMP/casper/filesystem.squashfs" 2>/dev/null | grep -qi 'Compression.*zstd'; then
      echo "[verify-iso] FAIL squashfs uses zstd — casper panic on many rigs" >&2
      exit 9
    fi
  else
    echo "[verify-iso] FAIL could not extract casper from ISO (7z)" >&2
    exit 10
  fi
  rm -rf "$SQ_TMP" "$FSZ_TMP"
elif [[ "${VERIFY_ISO_STRICT:-1}" == "1" ]]; then
  echo "[verify-iso] FAIL: need p7zip-full + squashfs-tools for filesystem.size check" >&2
  exit 11
fi
if grep -aq '\.disk/info' "$ISO_PATH" 2>/dev/null || grep -aq 'HackMe OS' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] PASS .disk metadata"
fi
# Warn if ISO still uses zstd squashfs (causes casper panic exitcode=0x100 on many rigs).
if command -v isoinfo >/dev/null 2>&1; then
  SQ="$(mktemp)"
  isoinfo -i "$ISO_PATH" -x /casper/filesystem.squashfs 2>/dev/null >"$SQ" || true
  if [[ -s "$SQ" ]]; then
    if head -c 4 "$SQ" | grep -q 'hsqs'; then
      if unsquashfs -s "$SQ" 2>/dev/null | grep -qi 'Compression.*zstd'; then
        echo "[verify-iso] WARN: squashfs uses zstd — rebuild with xz for casper boot" >&2
      elif unsquashfs -s "$SQ" 2>/dev/null | grep -qi 'Compression.*xz'; then
        echo "[verify-iso] PASS squashfs compression xz (casper-safe)"
      fi
    fi
  fi
  rm -f "$SQ"
fi
if grep -aqE 'boot=casper.*noplymouth.*console=tty1' "$ISO_PATH" 2>/dev/null && ! grep -aq 'username=root' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] PASS recommended entry (text console, casper-safe user)"
elif grep -aq 'username=root' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] FAIL: username=root in ISO — casper panic exitcode=0x100; rebuild" >&2
  exit 12
elif grep -aq 'boot=casper quiet splash' "$ISO_PATH" 2>/dev/null; then
  echo "[verify-iso] WARN: old ISO uses quiet splash default — rebuild for black-screen fix" >&2
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
