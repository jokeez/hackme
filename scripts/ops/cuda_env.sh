#!/usr/bin/env bash
# Source from build scripts: export CUDA_HOME, CGO_*, LD_LIBRARY_PATH for native CUDA builds.
#   source scripts/ops/cuda_env.sh
set -euo pipefail

cuda_find_home() {
  local d
  for d in \
    "${CUDA_HOME:-}" \
    "${CUDA_PATH:-}" \
    /usr/local/cuda \
    /usr/local/cuda-12.8 \
    /usr/local/cuda-12.6 \
    /usr/local/cuda-12.4 \
    /usr/local/cuda-12.2 \
    /usr/local/cuda-12.8 \
    /usr/local/cuda-12.6 \
    /usr/local/cuda-12.0 \
    /opt/cuda \
    /usr/lib/cuda
  do
    [[ -n "$d" && -f "$d/include/nvrtc.h" ]] || continue
    printf '%s' "$d"
    return 0
  done
  # Debian/Ubuntu split packages: libnvrtc-dev → /usr/include/nvrtc.h (no full cuda tree)
  if [[ -f /usr/include/nvrtc.h && -f /usr/include/cuda.h ]]; then
    printf '%s' "/usr"
    return 0
  fi
  if [[ -f /usr/include/nvrtc.h ]]; then
    printf '%s' "/usr"
    return 0
  fi
  return 1
}

if ! CUDA_HOME="$(cuda_find_home)"; then
  echo "[cuda-env] CUDA toolkit not found (need nvrtc.h). Install from repo root:" >&2
  echo "  cd ~/Desktop/HackMe && sudo bash scripts/ops/install_cuda_dev_ubuntu.sh" >&2
  return 1 2>/dev/null || exit 1
fi

export CUDA_HOME
if [[ "$CUDA_HOME" == "/usr" ]]; then
  export PATH="${PATH:-}"
  export CGO_ENABLED=1
  export CGO_CFLAGS="${CGO_CFLAGS:-} -I/usr/include"
  export CGO_LDFLAGS="${CGO_LDFLAGS:-} -L/usr/lib/x86_64-linux-gnu -lcuda -lnvrtc -lcudart"
  export LD_LIBRARY_PATH="/usr/lib/x86_64-linux-gnu:${LD_LIBRARY_PATH:-}"
else
  export PATH="$CUDA_HOME/bin:${PATH:-}"
  export CGO_ENABLED=1
  export CGO_CFLAGS="${CGO_CFLAGS:-} -I${CUDA_HOME}/include"
  export CGO_LDFLAGS="${CGO_LDFLAGS:-} -L${CUDA_HOME}/lib64 -L${CUDA_HOME}/lib -lcuda -lnvrtc -lcudart"
  export LD_LIBRARY_PATH="${CUDA_HOME}/lib64:${CUDA_HOME}/lib:${LD_LIBRARY_PATH:-}"
fi

if command -v nvidia-smi >/dev/null 2>&1; then
  echo "[cuda-env] CUDA_HOME=$CUDA_HOME driver=$(nvidia-smi --query-gpu=driver_version --format=csv,noheader 2>/dev/null | head -1 || echo '?')"
else
  echo "[cuda-env] CUDA_HOME=$CUDA_HOME (nvidia-smi not in PATH)"
fi
