#!/usr/bin/env bash
# Print recommended pool worker GPU backend: cuda | opencl | cpu
# Respects HACKME_GPU_BACKEND, HACKME_GPU_DISABLE, HACKME_FORCE_OPENCL.
set -euo pipefail

truthy() {
  local v
  v="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
  [[ "$v" == "1" || "$v" == "true" || "$v" == "yes" || "$v" == "on" ]]
}

nvidia_driver_ok() {
  command -v nvidia-smi >/dev/null 2>&1 || return 1
  nvidia-smi -L >/dev/null 2>&1
}

export_cuda_lib_path_for_root() {
  local root="$1"
  local libdir=""
  for libdir in "${root}/lib" "${root}/lib/cuda" "${root}/.deps/cuda-lib"; do
    if [[ -e "${libdir}/libnvrtc.so.12" || -e "${libdir}/libnvrtc.so" ]]; then
      export LD_LIBRARY_PATH="${libdir}${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
      return 0
    fi
  done
  return 0
}

cuda_worker_usable() {
  local root="$1"
  local bin=""
  export_cuda_lib_path_for_root "$root"
  for bin in "${root}/bin/workerpoh-cuda" "${root}/workerpoh-cuda"; do
    [[ -x "$bin" ]] || continue
    if command -v timeout >/dev/null 2>&1; then
      if timeout 12 env HACKME_REPO_ROOT="$root" LD_LIBRARY_PATH="${LD_LIBRARY_PATH:-}" "$bin" -h >/dev/null 2>&1; then
        return 0
      fi
    elif LD_LIBRARY_PATH="${LD_LIBRARY_PATH:-}" "$bin" -h >/dev/null 2>&1; then
      return 0
    fi
    # Binary exists with CUDA tags; driver check is separate — treat as usable.
    return 0
  done
  return 1
}

if truthy "${HACKME_GPU_DISABLE:-0}"; then
  echo cpu
  exit 0
fi

# Resolve repo root early (release tarball root or git checkout).
_detect_root="${HACKME_REPO_ROOT:-}"
if [[ -z "$_detect_root" ]]; then
  _detect_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fi

if [[ -n "${HACKME_GPU_BACKEND:-}" && "${HACKME_GPU_BACKEND}" != "auto" ]]; then
  req="$(printf '%s' "${HACKME_GPU_BACKEND}" | tr '[:upper:]' '[:lower:]')"
  if [[ "$req" == "cuda" ]]; then
    if nvidia_driver_ok && cuda_worker_usable "$_detect_root"; then
      echo cuda
      exit 0
    fi
    # Env asks for cuda but binary/driver missing — fall through (do not force cuda).
  elif [[ "$req" == "opencl" || "$req" == "cpu" ]]; then
    echo "$req"
    exit 0
  else
    echo "${HACKME_GPU_BACKEND}"
    exit 0
  fi
fi

if truthy "${HACKME_FORCE_OPENCL:-0}"; then
  echo opencl
  exit 0
fi

# NVIDIA: CUDA only when driver is healthy AND workerpoh-cuda is present.
if nvidia_driver_ok; then
  if cuda_worker_usable "$_detect_root"; then
    echo cuda
    exit 0
  fi
fi

# AMD / Intel / other: OpenCL (Mesa rusticl, ROCm, Intel compute runtime).
opencl_gpu_count() {
  if ! command -v clinfo >/dev/null 2>&1; then
    echo 0
    return
  fi
  clinfo 2>/dev/null | awk '/Number of devices/ {print $4; exit}'
}

has_opencl_gpu() {
  local n
  if command -v clinfo >/dev/null 2>&1; then
    n="$(opencl_gpu_count)"
    if [[ -n "$n" && "$n" -gt 0 ]]; then
      return 0
    fi
    return 1
  fi
  local f v
  for f in /sys/class/drm/card*/device/vendor; do
    [[ -f "$f" ]] || continue
    v="$(cat "$f" 2>/dev/null || true)"
    case "$v" in
      0x1002|4098|0x8086|32902|0x10de|4318) return 0 ;; # AMD, Intel, NVIDIA
    esac
  done
  return 1
}

if has_opencl_gpu; then
  echo opencl
  exit 0
fi

echo cpu
