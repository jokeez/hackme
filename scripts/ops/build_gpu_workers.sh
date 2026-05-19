#!/usr/bin/env bash
# Build all pool worker GPU variants (NVIDIA CUDA + OpenCL for AMD/Intel/others).
#   bash scripts/ops/build_gpu_workers.sh
#   bash scripts/ops/build_gpu_workers.sh --probe
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

PROBE=0
for arg in "$@"; do
  [[ "$arg" == "--probe" ]] && PROBE=1
done

export GOCACHE="${GOCACHE:-$ROOT/.cache/go-build}"
mkdir -p "$ROOT/bin" "$GOCACHE"

echo "[build-gpu] === NVIDIA CUDA (workerpoh-cuda) ==="
if bash "$ROOT/scripts/ops/build_cuda_worker.sh" $([[ "$PROBE" == "1" ]] && echo --probe); then
  echo "[build-gpu] CUDA OK"
else
  echo "[build-gpu] CUDA skipped or failed (no toolkit?)" >&2
fi

echo "[build-gpu] === OpenCL (AMD / Intel / fallback) ==="
if pkg-config --exists OpenCL 2>/dev/null || [[ -f /usr/include/CL/cl.h ]]; then
  echo "[build-gpu] go build -tags opencl -> $ROOT/bin/workerpoh-opencl"
  go build -trimpath -tags opencl -o "$ROOT/bin/workerpoh-opencl" ./cmd/workerpoh
  chmod 755 "$ROOT/bin/workerpoh-opencl"
  if go build -trimpath -tags opencl -o "$ROOT/bin/gpuprobe-opencl" ./tools/gpuprobe/ 2>/dev/null; then
    echo "[build-gpu] gpuprobe-opencl OK"
    [[ "$PROBE" == "1" ]] && HACKME_OPENCL_VERBOSE=1 "$ROOT/bin/gpuprobe-opencl" || true
  fi
  echo "[build-gpu] OpenCL OK"
else
  echo "[build-gpu] OpenCL headers missing — install: sudo apt install opencl-headers ocl-icd-opencl-dev" >&2
  echo "[build-gpu] AMD: mesa-opencl-icd / rusticl · Intel: intel-opencl-icd" >&2
fi

BACKEND="$(HACKME_REPO_ROOT="$ROOT" bash "$ROOT/scripts/ops/detect_gpu_backend.sh")"
echo "[build-gpu] detected backend for this host: $BACKEND"
case "$BACKEND" in
  cuda)
    ln -sf workerpoh-cuda "$ROOT/bin/workerpoh" 2>/dev/null || true
    ;;
  opencl)
    ln -sf workerpoh-opencl "$ROOT/bin/workerpoh" 2>/dev/null || true
    ;;
esac
echo "[build-gpu] done"
