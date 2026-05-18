#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
nvcc "${ROOT}/kernels/poh_search.cu" -ptx -o "${ROOT}/internal/gpupoh/poh_search.ptx" -arch=sm_75
echo "Wrote ${ROOT}/internal/gpupoh/poh_search.ptx"
