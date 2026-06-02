#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-0.1.0-rc11j}"
cd "$ROOT"
echo "[build-rc11j] CUDA workers (NVRTC-aware) + release bundle"
bash scripts/ops/build_cuda_worker.sh
bash scripts/ops/pack_vast_gpu_matrix.sh
VERSION="$VERSION" bash scripts/release/make_release_bundle.sh
echo "[build-rc11j] dist/release_${VERSION}"
