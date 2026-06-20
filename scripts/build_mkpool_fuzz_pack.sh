#!/usr/bin/env bash
# Build WASM property guards modeling mkpool stratum/SV2 parser boundaries.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/tasks/sources/security/mkpool"
OUT="$ROOT/tasks/artifacts/security/mkpool"
mkdir -p "$OUT"

command -v clang >/dev/null || { echo "clang required" >&2; exit 1; }

build_one() {
  local name="$1"
  local src="$SRC/${name}.c"
  local dst="$OUT/${name}.wasm"
  [[ -f "$src" ]] || { echo "missing $src" >&2; exit 1; }
  clang --target=wasm32 -O3 -nostdlib \
    -Wl,--no-entry -Wl,--export=check -Wl,--strip-all \
    -o "$dst" "$src"
  echo "built $dst ($(wc -c <"$dst") bytes)"
}

guards=(sv2_reader_bounds version_mask submit_hex_fields v1_line_frame)
for g in "${guards[@]}"; do
  build_one "$g"
done

echo "mkpool fuzz pack: ${#guards[@]} modules -> $OUT"
