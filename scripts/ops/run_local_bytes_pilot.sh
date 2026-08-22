#!/usr/bin/env bash
# Local bytes pilot gate — no prod deploy. Uses fuzz-sandbox + tracefuse/script guards.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

RUNS="${RUNS:-128}"
WORKERS="${WORKERS:-3}"
WASM="${WASM:-$ROOT/tasks/artifacts/security/rust_tracefuse_detector_bytes_guard.wasm}"

if [[ ! -f "$WASM" ]]; then
  echo "[local-bytes-pilot] building tracefuse bytes guard"
  rustc --target wasm32-unknown-unknown -O --crate-type=cdylib \
    "$ROOT/tasks/sources/security/rust_tracefuse_detector_bytes_guard.rs" \
    -o "$WASM"
fi

echo "[local-bytes-pilot] compare linear/guided runs=$RUNS workers=$WORKERS seed=auto"
go run ./cmd/fuzz-sandbox/ -compare -bytes -seed-profile=auto -runs "$RUNS" -workers "$WORKERS" -wasm "$WASM"

echo "[local-bytes-pilot] build customer template"
bash "$ROOT/scripts/ops/build_customer_bytes_guard.sh"

echo "[local-bytes-pilot] OK"
