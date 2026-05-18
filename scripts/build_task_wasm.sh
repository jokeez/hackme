#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC_DIR="$ROOT_DIR/tasks/sources"
OUT_DIR="$ROOT_DIR/tasks/artifacts"
MANIFEST_DIR="$ROOT_DIR/tasks/manifests"

mkdir -p "$OUT_DIR" "$MANIFEST_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing dependency: $1" >&2
    exit 1
  }
}

require_cmd sha256sum
require_cmd rustc
require_cmd clang
require_cmd go

RUST_SRC="$SRC_DIR/rust_check.rs"
CPP_SRC="$SRC_DIR/cpp_check.cpp"
RUST_WASM="$OUT_DIR/rust_check.wasm"
CPP_WASM="$OUT_DIR/cpp_check.wasm"

echo "Building Rust wasm..."
rustc --target wasm32-unknown-unknown -O --crate-type=cdylib "$RUST_SRC" -o "$RUST_WASM"

echo "Building C++ wasm..."
clang --target=wasm32 -O3 -nostdlib -Wl,--no-entry -Wl,--export=check -Wl,--strip-all -o "$CPP_WASM" "$CPP_SRC"

echo "Validating task ABI (check(i64)->i32) ..."
go run ./tools/task_abi_check "$RUST_WASM" "$CPP_WASM"

RUST_HASH="$(sha256sum "$RUST_WASM" | awk '{print $1}')"
CPP_HASH="$(sha256sum "$CPP_WASM" | awk '{print $1}')"

cat > "$MANIFEST_DIR/order-rust-001.json" <<EOF
{
  "id":"order-rust-001",
  "kind":"synthetic_poh_v1",
  "reward_hmc":0.02,
  "difficulty_score":20,
  "target_solves":3,
  "payer_ref":"company:rust-demo",
  "wasm_artifact_path":"rust_check.wasm",
  "artifact_hash":"$RUST_HASH"
}
EOF

cat > "$MANIFEST_DIR/order-cpp-001.json" <<EOF
{
  "id":"order-cpp-001",
  "kind":"synthetic_poh_v1",
  "reward_hmc":0.02,
  "difficulty_score":20,
  "target_solves":3,
  "payer_ref":"company:cpp-demo",
  "wasm_artifact_path":"cpp_check.wasm",
  "artifact_hash":"$CPP_HASH"
}
EOF

echo "Linting generated manifests ..."
go run ./tools/task_manifest_lint "$MANIFEST_DIR/order-rust-001.json" "$MANIFEST_DIR/order-cpp-001.json"

echo
echo "Done."
echo "Artifacts:"
echo "  $RUST_WASM"
echo "  $CPP_WASM"
echo "Hashes:"
echo "  rust_check.wasm: $RUST_HASH"
echo "  cpp_check.wasm:  $CPP_HASH"
echo "Manifests:"
echo "  $MANIFEST_DIR/order-rust-001.json"
echo "  $MANIFEST_DIR/order-cpp-001.json"
