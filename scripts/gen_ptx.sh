#!/usr/bin/env bash
# Optional offline PTX (runtime uses NVRTC from embedded poh_search.cu by default).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ARCH="${CUDA_ARCH:-sm_120}"
nvcc "${ROOT}/kernels/poh_search.cu" -ptx -arch="${ARCH}" -o "${ROOT}/internal/gpupoh/poh_search.ptx"
echo "Wrote ${ROOT}/internal/gpupoh/poh_search.ptx (arch=${ARCH})"
