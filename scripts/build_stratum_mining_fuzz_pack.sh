#!/usr/bin/env bash
# Build WASM property guards for stratum-mining/stratum Sv2 reference paths.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/tasks/sources/security/stratum_mining"
OUT="$ROOT/tasks/artifacts/security/stratum_mining"
mkdir -p "$OUT"

command -v clang >/dev/null || { echo "clang required" >&2; exit 1; }

build_one() {
  local name="$1"
  clang --target=wasm32 -O3 -nostdlib \
    -Wl,--no-entry -Wl,--export=check -Wl,--strip-all \
    -o "$OUT/${name}.wasm" "$SRC/${name}.c"
  echo "built $OUT/${name}.wasm"
}

guards=(sv2_frame_bounds extranonce_len user_identity_len open_channel_target)
for g in "${guards[@]}"; do build_one "$g"; done
echo "stratum_mining fuzz pack: ${#guards[@]} modules -> $OUT"
