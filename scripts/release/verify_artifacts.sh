#!/usr/bin/env bash
set -euo pipefail

# Verify release checksums and expected artifacts.
#
# Usage:
#   bash scripts/release/verify_artifacts.sh dist/release_1.0.0

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <release_dir>" >&2
  exit 2
fi

REL_DIR="$1"
if [[ ! -d "${REL_DIR}" ]]; then
  echo "[verify] release dir not found: ${REL_DIR}" >&2
  exit 2
fi

cd "${REL_DIR}"

for f in SHA256SUMS.txt BUILD_INFO.txt; do
  if [[ ! -f "${f}" ]]; then
    echo "[verify] missing file: ${f}" >&2
    exit 1
  fi
done

if [[ ! -f "RELEASE_MANIFEST.json" ]]; then
  echo "[verify] WARN: RELEASE_MANIFEST.json missing (legacy bundle format)"
fi

echo "[verify] sha256sum -c SHA256SUMS.txt"
sha256sum -c SHA256SUMS.txt

if ! ls hackme_*_windows.zip >/dev/null 2>&1; then
  echo "[verify] windows zip missing" >&2
  exit 1
fi
if ! ls hackme_*_linux.tar.gz >/dev/null 2>&1; then
  echo "[verify] linux tar missing" >&2
  exit 1
fi

if [[ -f "RELEASE_MANIFEST.json" ]] && command -v jq >/dev/null 2>&1; then
  if ! jq -e '.version != null and (.artifacts|type=="array") and (.artifacts|length==2)' RELEASE_MANIFEST.json >/dev/null; then
    echo "[verify] RELEASE_MANIFEST.json has invalid shape" >&2
    exit 1
  fi
fi

echo "[verify] PASS: artifacts look consistent"
