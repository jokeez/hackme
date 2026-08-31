#!/usr/bin/env bash
# Gate: Hunt trim + havoc mutator (interesting, dict-ops, autodict) — no coverage.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

echo "[hunt-mutator-trim-gate] unit tests"
go test -count=1 ./internal/fuzzengine/... ./internal/fuzzupstream/... ./internal/hunt/... \
  -run 'Trim|Autodict|Interesting|MutateBytes|MutateWithDict|SanitizerSame|ReproCmdHunt|ParseDictTokens|TestCollect|TestBuildInventoryHarnessCjson' \
  -timeout=5m

pass "hunt_mutator_trim_gate PASS"
