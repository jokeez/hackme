#!/usr/bin/env bash
# Gate: Hunt HTML report scope + deliverable URLs + ASAN crash replay path.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

echo "[hunt-report-gate] unit tests"
go test -count=1 . ./internal/hunt/... ./internal/poolfuzz/... ./internal/workerfuzzloop/... -run 'TestHuntReportE2E|TestRenderFuzzReportHTML_HuntScope|TestHuntCampaignCreate5050|TestReplayShard|TestShardSegment|TestEvalHuntSubmit|TestHuntShard|TestHuntGuided|TestHuntCorpus' -timeout=5m

pass "hunt_report_gate PASS"
