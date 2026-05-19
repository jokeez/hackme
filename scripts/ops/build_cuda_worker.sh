#!/usr/bin/env bash
# Production build: native CUDA workerpoh (+ optional OpenCL fallback in same tree).
#   bash scripts/ops/build_cuda_worker.sh
#   bash scripts/ops/build_cuda_worker.sh --probe
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

PROBE=0
TAGS="cuda,opencl"
OUT_CUDA="$ROOT/bin/workerpoh-cuda"
OUT_OCL="$ROOT/bin/workerpoh-opencl"
OUT_DEFAULT="$ROOT/bin/workerpoh"

for arg in "$@"; do
  case "$arg" in
    --probe) PROBE=1 ;;
  esac
done

# shellcheck source=scripts/ops/cuda_env.sh
source "$ROOT/scripts/ops/cuda_env.sh"

export GOCACHE="${GOCACHE:-$ROOT/.cache/go-build}"
mkdir -p "$ROOT/bin" "$GOCACHE"

echo "[build-cuda] go build -tags ${TAGS} -> $OUT_CUDA"
if ! go build -trimpath -tags "$TAGS" -o "$OUT_CUDA" ./cmd/workerpoh; then
  echo "[build-cuda] FAILED — check CGO_CFLAGS/LDFLAGS and nvrtc.h" >&2
  exit 1
fi
chmod 755 "$OUT_CUDA"
ln -sf "$(basename "$OUT_CUDA")" "$OUT_DEFAULT"

# OpenCL-only binary for rigs without NVRTC (MSK CPU nodes skip this)
if pkg-config --exists OpenCL 2>/dev/null || [[ -f /usr/include/CL/cl.h ]]; then
  echo "[build-cuda] go build -tags opencl -> $OUT_OCL"
  go build -trimpath -tags opencl -o "$OUT_OCL" ./cmd/workerpoh
  chmod 755 "$OUT_OCL"
fi

if [[ "$PROBE" == "1" ]]; then
  echo "[build-cuda] running GPU probe..."
  HACKME_CUDA_VERBOSE=1 "$OUT_CUDA" -h 2>&1 | head -3 || true
  if go build -trimpath -tags cuda -o "$ROOT/bin/gpuprobe-cuda" ./tools/gpuprobe/ 2>/dev/null; then
    "$ROOT/bin/gpuprobe-cuda" || true
  fi
fi

echo "[build-cuda] OK: $OUT_CUDA ($(file -b "$OUT_CUDA" 2>/dev/null || echo binary))"
echo "[build-cuda] set HACKME_GPU_BACKEND=cuda and use bin/workerpoh-cuda (symlink: bin/workerpoh)"
