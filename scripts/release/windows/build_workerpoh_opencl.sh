#!/usr/bin/env bash
# Build workerpoh-opencl.exe for Windows (mingw cross via Docker, or native mingw on host).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
OUT="${1:-${ROOT}/dist/workerpoh-opencl.exe}"
mkdir -p "$(dirname "$OUT")"

build_host_mingw() {
  if ! command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1; then
    return 1
  fi
  if ! [[ -f /usr/include/CL/cl.h ]]; then
    echo "[opencl-win] host mingw: install opencl-headers (CL/cl.h missing)" >&2
    return 1
  fi
  echo "[opencl-win] host mingw cross-compile -> $OUT"
  (
    cd "$ROOT"
    CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
      CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
      go build -tags opencl -trimpath -ldflags "-s -w" -o "$OUT" ./cmd/workerpoh
  )
}

build_docker() {
  echo "[opencl-win] docker build (mingw + OpenCL headers)"
  docker build -f "${ROOT}/scripts/release/windows/Dockerfile.opencl-mingw" -t hackme-opencl-mingw "$ROOT"
  cid="$(docker create hackme-opencl-mingw)"
  docker cp "${cid}:/out/workerpoh-opencl.exe" "$OUT"
  docker rm -f "${cid}" >/dev/null
}

if build_host_mingw 2>/dev/null; then
  :
elif command -v docker >/dev/null 2>&1; then
  build_docker
else
  echo "[opencl-win] need docker or mingw-w64 + opencl-headers" >&2
  exit 1
fi

ls -la "$OUT"
file "$OUT" || true
echo "[opencl-win] OK: $OUT"
