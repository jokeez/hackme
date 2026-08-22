#!/usr/bin/env bash
# Build customer check_bytes guard template to WASM (local / pilot prep).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SRC="${SRC:-$ROOT/tasks/sources/security/rust_customer_bytes_guard_template.rs}"
OUT="${OUT:-$ROOT/tasks/artifacts/security/rust_customer_bytes_guard_template.wasm}"
mkdir -p "$(dirname "$OUT")"
echo "[build-bytes-guard] $SRC -> $OUT"
rustc --target wasm32-unknown-unknown -O --crate-type=cdylib "$SRC" -o "$OUT"
echo "[build-bytes-guard] OK $(wc -c < "$OUT") bytes"
