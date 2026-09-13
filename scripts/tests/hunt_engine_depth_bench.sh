#!/usr/bin/env bash
# Compare mutation depth uniqueness for Hunt engine calibration.
# Usage: bash scripts/tests/hunt_engine_depth_bench.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
echo "== fuzzengine unit + depth bench =="
go test ./internal/fuzzengine/ ./internal/hunt/ -count=1 -timeout 120s
echo
echo "== MeasureMutationDepth (5000 samples) =="
go test ./internal/fuzzengine/ -run TestMeasureMutationDepthUniqueRatio -v -count=1
echo
echo "== Finding family + explore_v2 =="
go test ./internal/fuzzengine/ -run 'TestFindingFamily|TestCorpusExplore|TestHavocStack' -v -count=1
echo
echo "== A/B vs upstream baseline (5000 samples) =="
go test ./internal/fuzzengine/ -run TestEngineABComparison -v -count=1
echo "== Coverage feedback + power schedule v2.4 =="
go test ./internal/fuzzengine/ -run 'TestCoverageFeedback|TestCoverageBucketsStructural|TestSeedSchedule|TestPowerSchedule|TestDecay|TestMeasureGuided|TestCompact|TestRank' -v -count=1
echo "OK — engine v2.4 depth + coverage + power schedule gates passed"
