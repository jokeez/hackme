#!/usr/bin/env bash
# Safely drop ISO build work dirs (unmount chroot vfs before rm).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
VERSION="${VERSION:-0.1.0-rc11i}"
WORK="${WORK:-${ROOT}/.cache/iso-work-${VERSION}}"
CHROOT="${WORK}/chroot"

if [[ -d "$CHROOT" ]]; then
  for m in proc sys dev/pts dev; do
    target="${CHROOT}/${m}"
    if mountpoint -q "$target" 2>/dev/null; then
      umount -l "$target" 2>/dev/null || umount "$target" 2>/dev/null || true
    fi
  done
fi
rm -rf "$WORK"
echo "[iso-clean] removed $WORK"
