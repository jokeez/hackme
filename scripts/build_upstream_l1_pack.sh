#!/usr/bin/env bash
# Build upstream-ported L1 WASM guards (real excerpt logic, not synthetic guards).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UP="$ROOT/tasks/sources/security/upstream"
OUT="$ROOT/tasks/artifacts/security"

mkdir -p "$OUT"
command -v clang >/dev/null || { echo "need clang" >&2; exit 1; }

build_one() {
  local name="$1"
  local src="$UP/${name}.c"
  local out="$OUT/upstream_${name}.wasm"
  [[ -f "$src" ]] || { echo "missing $src" >&2; exit 1; }
  clang --target=wasm32 -O2 -nostdlib \
    -I"$UP" \
    -Wl,--no-entry -Wl,--export=check -Wl,--strip-all \
    -o "$out" "$src"
  echo "  $out"
}

echo "Building upstream L1 WASM pack..."
for t in \
  bitcoin_getscriptop \
  bitcoin_hasvalidops \
  bitcoin_tx_check \
  bitcoin_tx_dup_inputs \
  bitcoin_evalscript_push \
  ethereum_value_overflow \
  dogecoin_hasvalidops \
  litecoin_getscriptop \
  hackme_order_gate; do
  build_one "$t"
done
echo "Done → $OUT/upstream_*.wasm"
