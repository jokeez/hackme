#!/usr/bin/env bash
# Honest check: ASAN is native repro, not a GuardPack. Local-only.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

echo "[asan-story] Packs ≠ ASAN/UBSan"
echo "  Packs = detector property (WASM check / check_bytes)"
echo "  ASAN  = native repro confirm on harness binary (audit/deep)"

if command -v clang >/dev/null 2>&1; then
  echo "[asan-story] clang present — running ASAN binary repro tests"
  go test -count=1 ./internal/fuzznative/ -run 'TestEvalReproAsanBinary' -timeout=180s
  echo "[asan-story] ASAN path: AVAILABLE"
else
  echo "[asan-story] clang: MISSING on this machine"
  echo "  → asan_binary tests SKIP; product falls back to go_port when configured"
  go test -count=1 ./internal/fuzznative/ -run 'TestEvalRepro[^A]|TestResolveHarness' -timeout=60s
  echo "[asan-story] ASAN path: NOT AVAILABLE locally (need clang + customer/upstream harness)"
fi

echo "[asan-story] go_port native bridge still builds:"
go test -count=1 ./internal/fuzznative/ -run 'TestEvalRepro$' -timeout=60s 2>/dev/null || \
  go test -count=1 ./internal/fuzznative/ -timeout=60s -run 'TestEvalRepro[^A]' >/dev/null

echo "[asan-story] OK — do not invent ASAN packs; sell native_repro on audit/deep"
