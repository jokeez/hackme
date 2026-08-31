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

echo "[product-final-gate] site b2b content"
bash "$ROOT/scripts/tests/site_b2b_content_gate.sh"

echo "[product-final-gate] coordinator corpus sync"
bash "$ROOT/scripts/tests/coordinator_corpus_sync_gate.sh"

echo "[product-final-gate] hunt escrow + severity tiers"
go test -count=1 ./internal/fuzzescrow/... ./internal/chain/... -run 'Hunt|Bounty' -timeout=60s

echo "[product-final-gate] hunt report html"
bash "$ROOT/scripts/tests/hunt_report_gate.sh"

echo "[product-final-gate] hunt harness publish"
bash "$ROOT/scripts/tests/hunt_harness_publish_gate.sh"

pass "product_final_gate PASS"
