#!/usr/bin/env bash
# Build Scan-tier smoke guard WASM aliases (bounds / overflow / state).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SEC_SRC="$ROOT/tasks/sources/security"
OUT_DIR="$ROOT/tasks/artifacts/security"
mkdir -p "$OUT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[scan-smoke] missing dependency: $1" >&2
    exit 1
  }
}

require_cmd rustc

build_alias() {
  local src_name="$1"
  local out_name="$2"
  local src="$SEC_SRC/rust_${src_name}.rs"
  local out="$OUT_DIR/rust_${out_name}.wasm"
  if [[ ! -f "$src" ]]; then
    echo "[scan-smoke] missing source: $src" >&2
    exit 1
  fi
  echo "[scan-smoke] $src -> $out"
  rustc \
    --target wasm32-unknown-unknown \
    -C panic=abort -C opt-level=z -C lto=fat \
    --crate-type=cdylib \
    "$src" -o "$out"
}

build_alias bounds_guard bounds_smoke_guard
build_alias overflow_guard overflow_smoke_guard
build_alias state_transition_guard state_smoke_guard

echo "[scan-smoke] OK — $(wc -c <"$OUT_DIR/rust_bounds_smoke_guard.wasm") bytes bounds_smoke"
