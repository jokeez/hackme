#!/usr/bin/env bash
# Compare current mining vs reports/baseline-2d-LATEST (or pass dir as $1).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASELINE_DIR="${1:-$ROOT/reports/baseline-2d-LATEST}"
exec bash "$ROOT/scripts/ops/compare_baseline_4h.sh" "$BASELINE_DIR"
