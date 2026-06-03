#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-0.1.0-rc11k}"
cd "$ROOT"
echo "[build-rc11k] launch candidate bundle (commit=$(git rev-parse --short=12 HEAD 2>/dev/null || echo nogit))"
bash scripts/ops/build_cuda_worker.sh
if [[ "${SKIP_VAST_PACK:-0}" != "1" ]]; then
  bash scripts/ops/pack_vast_gpu_matrix.sh --skip-build 2>/dev/null || echo "[build-rc11k] WARN: vast pack skipped"
fi
VERSION="$VERSION" bash scripts/release/make_release_bundle.sh
echo "[build-rc11k] done: dist/release_${VERSION}"
