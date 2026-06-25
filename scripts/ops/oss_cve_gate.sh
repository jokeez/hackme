#!/usr/bin/env bash
# Gate: OSS CVE upstream fuzz pipeline.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[oss-cve-gate] unit tests"
go test ./internal/fuzzupstream/... -count=1 -timeout 20m

echo "[oss-cve-gate] smoke hunt jsmn (fast target)"
TARGETS=jsmn BUDGET=3000 TIME_LIMIT=45 OUT="$ROOT/reports/oss-cve-gate-smoke" \
  bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh"

echo "[oss-cve-gate] PASS"
