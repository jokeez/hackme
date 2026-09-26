#!/usr/bin/env bash
# Issue #13 C/D/E gate — what hub already shipped vs FounderB follow-through.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "== C: Hunt Watch honesty 2.0 (hub) =="
if [[ -f scripts/tests/hunt_watch_honesty_gate.sh && -d reports/hunt-watch/2026sep ]]; then
  bash scripts/tests/hunt_watch_honesty_gate.sh
else
  echo "SKIP watch series artifacts (optional on contributor machine)"
fi

echo "== D: customer-repo fail-closed =="
go test ./internal/hunt/ -count=1 -timeout 120s \
  -run 'TestRustBuildRefusePackageStubWithoutFuzzTarget|TestInventoryBuildRefuseMissingLLVMFuzzerWithoutTemplate'

echo "== E: GPU Dig mutators (CPU eval — not GPU ASAN) =="
go test ./internal/gpudig/ -count=1 -timeout 60s

echo "== engine v2.10 =="
go test ./internal/fuzzengine/ -count=1 -timeout 180s \
  -run 'TestEngineABComparison|TestDeepHavocV28|TestFindingFamily'

echo "OK — issue #13 C/D/E FounderB gate"
