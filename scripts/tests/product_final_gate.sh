#!/usr/bin/env bash
# Pre-release product gate: Dig/Scan + Hunt + corpus persist (no merge ritual).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

echo "[product-final-gate] scan smoke wasm"
bash "$ROOT/scripts/ops/build_scan_smoke_guards.sh"

echo "[product-final-gate] unit suites"
go test -count=1 ./internal/hunt/... ./internal/fuzzengine/... ./internal/fuzzingcli/... ./internal/poolfuzz/... -timeout=120s

echo "[product-final-gate] corpus persist"
bash "$ROOT/scripts/tests/corpus_persist_gate.sh"

echo "[product-final-gate] hunt pool smoke"
bash "$ROOT/scripts/tests/hunt_pool_smoke_gate.sh"

echo "[product-final-gate] hunt harness publish"
bash "$ROOT/scripts/tests/hunt_harness_publish_gate.sh"

pass "product_final_gate PASS"
