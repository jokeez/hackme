#!/usr/bin/env bash
# Gate: cross-campaign corpus persist — export on observe, import on second campaign register.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

echo "[corpus-persist-gate] build scan smoke wasm"
bash "$ROOT/scripts/ops/build_scan_smoke_guards.sh"

for wasm in \
  rust_bounds_smoke_guard.wasm \
  rust_overflow_smoke_guard.wasm \
  rust_state_smoke_guard.wasm; do
  [[ -f "$ROOT/tasks/artifacts/security/$wasm" ]] || fail "missing $wasm after build"
done

echo "[corpus-persist-gate] unit + e2e"
go test -count=1 ./internal/fuzzengine/... -run 'CorpusPersist' -timeout=30s
go test -count=1 ./internal/poolfuzz/... -run 'TestCorpusPersistCrossCampaignE2E' -timeout=60s

pass "corpus_persist_gate PASS"
