#!/usr/bin/env bash
# Gate: Dig depth enhancements — power scheduling, external seeds, report profile.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

go test -count=1 ./internal/fuzzingcli -run 'TestApplyDig|TestFinalizeDig|TestDigDepth|TestLoadDig|TestMergeDig' -timeout 3m
go test -count=1 . -run 'TestBuildHumanSummaryAndVerdict' -timeout 2m

bash "$ROOT/scripts/tests/hunt_corpus_import_gate.sh" >/dev/null 2>&1 || true
go test -count=1 ./internal/fuzzingcli -run TestApplyPackConfigSecrets -timeout 2m

echo "[dig-depth-gate] PASS"
