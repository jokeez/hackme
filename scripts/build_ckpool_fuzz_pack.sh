#!/usr/bin/env bash
# Build WASM property guards modeling ckpool (Con Kolivas) stratum parser boundaries.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/tasks/sources/security/ckpool"
OUT="$ROOT/tasks/artifacts/security/ckpool"
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

guards=(v1_line_frame submit_hex_fields version_mask submit_param_count ntime_window)
for g in "${guards[@]}"; do
  build_one "$g"
done

echo "ckpool fuzz pack: ${#guards[@]} modules -> $OUT"
