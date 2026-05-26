#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-0.1.0-rc11i}"
cd "$ROOT"
VERSION="$VERSION" bash scripts/release/make_release_bundle.sh
echo "[build-rc11i] dist/release_${VERSION}"
