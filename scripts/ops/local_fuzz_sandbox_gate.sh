#!/usr/bin/env bash
# Local gate: fuzz engine + poolfuzz unit tests + sandbox A/B (no VPS).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
echo "[local-fuzz-gate] go test fuzzengine poolfuzz store"
go test -count=1 -timeout=180s ./internal/fuzzengine/... ./internal/poolfuzz/... ./internal/store/...
echo "[local-fuzz-gate] build fuzz-sandbox"
go build -o /tmp/fuzz-sandbox ./cmd/fuzz-sandbox
echo "[local-fuzz-gate] sandbox u64 compare runs=128"
/tmp/fuzz-sandbox -compare -runs 128 -workers 3
echo "[local-fuzz-gate] sandbox bytes compare runs=64"
/tmp/fuzz-sandbox -compare -runs 64 -workers 2 -bytes
echo "[local-fuzz-gate] sandbox drain test (runs=200 > queue_depth)"
go test -count=1 -timeout=120s ./internal/poolfuzz/ -run TestLocalDrainCampaignExceedsQueueDepth
echo "[local-fuzz-gate] CVE guard direct tests"
go test -count=1 -timeout=60s ./internal/sandbox/ -run 'Fluxtap|ScriptPush'
echo "[local-fuzz-gate] project matrix (POC guards)"
go run ./tools/project_fuzz_matrix/
echo "[local-fuzz-gate] OK"
