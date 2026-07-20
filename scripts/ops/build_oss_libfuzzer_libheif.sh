#!/usr/bin/env bash
# Build libheif file_fuzzer (ASAN + libFuzzer) from upstream WITH_FUZZERS cmake target.
#
#   bash scripts/ops/build_oss_libfuzzer_libheif.sh
#   FUZZER=file_fuzzer bash scripts/ops/build_oss_libfuzzer_libheif.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

FUZZER="${FUZZER:-file_fuzzer}"
REPO="${LIBHEIF_REPO:-https://github.com/strukturag/libheif.git}"
REF="${LIBHEIF_REF:-master}"
CLONE="${LIBHEIF_CLONE:-$ROOT/.cache/oss-cve-clones/libheif}"
BUILD="${LIBHEIF_BUILD:-$CLONE/build-fuzz-asan}"
OUT_DIR="$ROOT/.cache/oss-libfuzzer-bin"
OUT="$OUT_DIR/libheif-${FUZZER}-asan"
STAMP="$OUT_DIR/.libheif-${FUZZER}-build.stamp"

log() { echo "[libheif-fuzz-build $(date -u +%H:%M:%S)] $*" >&2; }

if [[ "${SKIP_REBUILD:-0}" == "1" && -x "$OUT" ]]; then
  log "reuse $OUT"
  echo "$OUT"
  exit 0
fi

command -v clang >/dev/null || { log "need clang"; exit 1; }
command -v clang++ >/dev/null || { log "need clang++"; exit 1; }
command -v git >/dev/null || { log "need git"; exit 1; }

CMAKE="${CMAKE:-}"
if [[ -z "$CMAKE" ]]; then
  if command -v cmake >/dev/null; then
    CMAKE=cmake
  elif [[ -x "$ROOT/.cache/tools/cmake-3.29.0-linux-x86_64/bin/cmake" ]]; then
    CMAKE="$ROOT/.cache/tools/cmake-3.29.0-linux-x86_64/bin/cmake"
  else
    log "need cmake (install or set CMAKE=...)"
    exit 1
  fi
fi

mkdir -p "$(dirname "$CLONE")" "$OUT_DIR" "$ROOT/logs"
if [[ ! -d "$CLONE/.git" ]]; then
  log "clone $REPO"
  git clone --depth=1 --branch "$REF" "$REPO" "$CLONE"
fi

mkdir -p "$BUILD"
log "cmake configure → $BUILD"
"$CMAKE" -S "$CLONE" -B "$BUILD" \
  -DCMAKE_BUILD_TYPE=RelWithDebInfo \
  -DBUILD_SHARED_LIBS=OFF \
  -DWITH_FUZZERS=ON \
  -DWITH_EXAMPLES=OFF \
  -DWITH_GDK_PIXBUF=OFF \
  -DWITH_RAV1E=OFF \
  -DWITH_SvtEnc=OFF \
  -DWITH_AOM_DECODER=OFF \
  -DWITH_AOM_ENCODER=OFF \
  -DWITH_X265=OFF \
  -DWITH_LIBDE265=ON \
  -DWITH_DAV1D=ON \
  >>"$ROOT/logs/libheif-fuzz-build.log" 2>&1

log "build target $FUZZER"
"$CMAKE" --build "$BUILD" --target "$FUZZER" -j"$(nproc 2>/dev/null || echo 2)" \
  >>"$ROOT/logs/libheif-fuzz-build.log" 2>&1

BIN="$BUILD/fuzzing/$FUZZER"
[[ -x "$BIN" ]] || { log "missing $BIN"; exit 1; }
cp -f "$BIN" "$OUT"
chmod +x "$OUT"
date -u +%Y-%m-%dT%H:%M:%SZ >"$STAMP"
log "PASS $OUT ($(du -h "$OUT" | awk '{print $1}'))"
echo "$OUT"
