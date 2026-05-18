#!/usr/bin/env bash
set -euo pipefail

# Nightly-style release pipeline:
# - run tests
# - build bundles
# - verify checksums
#
# Usage:
#   VERSION=1.0.0-rc2 bash scripts/release/release_nightly.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

VERSION="${VERSION:-nightly_$(date -u +%Y%m%dT%H%M%SZ)}"

echo "[nightly] tests"
go test ./...

echo "[nightly] build bundles for ${VERSION}"
VERSION="${VERSION}" bash scripts/release/make_release_bundle.sh

DIST_DIR="${ROOT_DIR}/dist/release_${VERSION}"
echo "[nightly] verify checksums"
bash scripts/release/verify_artifacts.sh "${DIST_DIR}"

echo "[nightly] PASS"
echo "[nightly] output: ${DIST_DIR}"
