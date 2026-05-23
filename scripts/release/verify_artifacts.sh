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
  if ! jq -e '.version != null and (.artifacts|type=="array") and (.artifacts|length>=2)' RELEASE_MANIFEST.json >/dev/null; then
    echo "[verify] RELEASE_MANIFEST.json has invalid shape" >&2
    exit 1
  fi
  if [[ -f SHA256SUMS-iso.txt ]]; then
    iso_file="$(jq -r '.artifacts[]|select(.platform=="hackme-os")|.file // empty' RELEASE_MANIFEST.json)"
    iso_manifest_sha="$(jq -r '.artifacts[]|select(.platform=="hackme-os")|.sha256 // empty' RELEASE_MANIFEST.json)"
    iso_sums_sha="$(awk -v f="$iso_file" '$NF==f {print $1; exit}' SHA256SUMS-iso.txt)"
    if [[ -n "$iso_file" && -n "$iso_manifest_sha" && "$iso_manifest_sha" != "$iso_sums_sha" ]]; then
      echo "[verify] ISO sha mismatch: manifest=$iso_manifest_sha sums=$iso_sums_sha" >&2
      echo "[verify] run: bash scripts/release/refresh_release_manifest.sh" >&2
      exit 1
    fi
    if [[ -f "$iso_file" ]]; then
      iso_actual="$(sha256sum "$iso_file" | awk '{print $1}')"
      if [[ "$iso_actual" != "$iso_sums_sha" ]]; then
        echo "[verify] ISO file sha mismatch (rebuild or refresh sums)" >&2
        exit 1
      fi
      echo "[verify] ISO sha256 OK ($iso_file)"
    fi
  fi
fi

echo "[verify] PASS: artifacts look consistent"
