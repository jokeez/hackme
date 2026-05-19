#!/usr/bin/env bash
# Print recommended pool worker GPU backend: cuda | opencl | cpu
# Respects HACKME_GPU_BACKEND, HACKME_GPU_DISABLE, HACKME_FORCE_OPENCL.
set -euo pipefail

truthy() {
  local v
  v="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
  [[ "$v" == "1" || "$v" == "true" || "$v" == "yes" || "$v" == "on" ]]
}

if truthy "${HACKME_GPU_DISABLE:-0}"; then
  echo cpu
  exit 0
fi

if [[ -n "${HACKME_GPU_BACKEND:-}" && "${HACKME_GPU_BACKEND}" != "auto" ]]; then
  echo "${HACKME_GPU_BACKEND}"
  exit 0
fi

if truthy "${HACKME_FORCE_OPENCL:-0}"; then
  echo opencl
  exit 0
fi

# NVIDIA: native CUDA when toolkit binary exists.
if command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi -L >/dev/null 2>&1; then
  root="${HACKME_REPO_ROOT:-}"
  if [[ -z "$root" ]]; then
    root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  fi
  if [[ -x "${root}/bin/workerpoh-cuda" ]] || command -v nvcc >/dev/null 2>&1 || [[ -f /usr/local/cuda/include/nvrtc.h ]]; then
    echo cuda
    exit 0
  fi
fi

# AMD / Intel / other: OpenCL (Mesa rusticl, ROCm, Intel compute runtime).
has_opencl_gpu() {
  if command -v clinfo >/dev/null 2>&1; then
    clinfo 2>/dev/null | awk 'BEGIN{ok=0} /Device Type[[:space:]]+GPU/{ok=1} END{exit ok?0:1}' && return 0
  fi
  local f v
  for f in /sys/class/drm/card*/device/vendor; do
    [[ -f "$f" ]] || continue
    v="$(cat "$f" 2>/dev/null || true)"
    case "$v" in
      0x1002|4098|0x8086|32902) return 0 ;; # AMD, Intel
    esac
  done
  return 1
}

if has_opencl_gpu; then
  echo opencl
  exit 0
fi

echo cpu
