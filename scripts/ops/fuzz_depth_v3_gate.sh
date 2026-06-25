#!/usr/bin/env bash
# Gate: fuzz depth v3 — byte corpus, native bridge, marketplace API.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[fuzz-depth-v3] unit tests"
go test ./internal/fuzzengine/... ./internal/fuzznative/... ./internal/poolfuzz/... -count=1

echo "[fuzz-depth-v3] sandbox InvokeCheckInput"
go test ./internal/sandbox/... -count=1 -run 'TestMinimalCheckWasm'

echo "[fuzz-depth-v3] schema migration"
go test ./internal/store/... -count=1 -run 'TestOpen|TestFuzzNativeQueue'

echo "[fuzz-depth-v3] PASS"
