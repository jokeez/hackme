#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SEC_SRC="$ROOT_DIR/tasks/sources/security"
OUT_DIR="$ROOT_DIR/tasks/artifacts/security"
MANIFEST_DIR="$ROOT_DIR/tasks/manifests/security"

mkdir -p "$OUT_DIR" "$MANIFEST_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing dependency: $1" >&2
    exit 1
  }
}

require_cmd rustc
require_cmd clang
require_cmd sha256sum

build_rust() {
  local name="$1"
  local src="$SEC_SRC/rust_${name}.rs"
  local out="$OUT_DIR/rust_${name}.wasm"
  rustc \
    --target wasm32-unknown-unknown \
    -C panic=abort -C opt-level=z -C lto=fat \
    --crate-type=cdylib \
    "$src" -o "$out"
}

build_cpp() {
  local name="$1"
  local src="$SEC_SRC/cpp_${name}.cpp"
  local out="$OUT_DIR/cpp_${name}.wasm"
  clang \
    --target=wasm32 \
    -O3 -nostdlib \
    -Wl,--no-entry -Wl,--export=check -Wl,--strip-all \
    -o "$out" "$src"
}

mk_manifest() {
  local id="$1"
  local payer="$2"
  local path="$3"
  local hash="$4"
  cat > "$MANIFEST_DIR/${id}.json" <<EOF
{
  "id":"$id",
  "kind":"synthetic_poh_v1",
  "reward_hmc":0.02,
  "difficulty_score":25,
  "target_solves":3,
  "payer_ref":"$payer",
  "wasm_artifact_path":"security/$(basename "$path")",
  "artifact_hash":"$hash"
}
EOF
}

tasks=(bounds_guard overflow_guard state_transition_guard script_push_bounds_guard)

echo "Building security task pack..."
for t in "${tasks[@]}"; do
  build_rust "$t"
  build_cpp "$t"
done

echo "Generating manifests..."
for t in "${tasks[@]}"; do
  rust_path="$OUT_DIR/rust_${t}.wasm"
  cpp_path="$OUT_DIR/cpp_${t}.wasm"
  rust_hash="$(sha256sum "$rust_path" | awk '{print $1}')"
  cpp_hash="$(sha256sum "$cpp_path" | awk '{print $1}')"
  mk_manifest "order-rust-${t}-001" "company:rust-${t}" "$rust_path" "$rust_hash"
  mk_manifest "order-cpp-${t}-001" "company:cpp-${t}" "$cpp_path" "$cpp_hash"
done

echo
echo "Done."
echo "Artifacts dir:  $OUT_DIR"
echo "Manifests dir:  $MANIFEST_DIR"
echo
echo "Submit examples:"
echo "  curl -s -X POST http://127.0.0.1:8080/api/tasks -H 'Content-Type: application/json' --data-binary @${MANIFEST_DIR}/order-rust-bounds_guard-001.json | jq"
echo "  curl -s -X POST http://127.0.0.1:8080/api/tasks -H 'Content-Type: application/json' --data-binary @${MANIFEST_DIR}/order-cpp-bounds_guard-001.json | jq"
