#!/usr/bin/env bash
# Gate: Fuzz Tier C — upstream_binary depth + ASAN harness repro.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[fuzz-tier-c] depth engine tests"
go test ./internal/fuzzengine/... -count=1 -run 'Depth|Tier|Repro'

echo "[fuzz-tier-c] native bridge + ASAN repro"
HACKME_REPO_ROOT="$ROOT" go test ./internal/fuzznative/... -count=1

echo "[fuzz-tier-c] pool bounty gate"
go test ./internal/poolfuzz/... -count=1

bash "$ROOT/scripts/ops/fuzz_depth_v3_gate.sh"

if command -v clang >/dev/null; then
  echo "[fuzz-tier-c] warm ASAN cache (optional)"
  HACKME_REPO_ROOT="$ROOT" go test ./internal/fuzznative/... -count=1 -run TestEvalReproAsanBinaryDupInputs
fi

echo "[fuzz-tier-c] PASS"
