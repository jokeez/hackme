#!/usr/bin/env bash
# One-shot repo health: formatting, static quality gate, same static language
# checks as CI (MODE=lang_static), then tests, vet, release-ish node build.
# Run from anywhere:  bash scripts/ops/verify_project_health.sh
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[verify] missing command: $1" >&2
    exit 1
  }
}

require_cmd go
require_cmd jq
require_cmd python3

echo "[verify] gofmt (no dirty Go files)"
bad_fmt="$(gofmt -l . || true)"
if [[ -n "$bad_fmt" ]]; then
  echo "[verify] FAIL: run gofmt -w on:" >&2
  echo "$bad_fmt" >&2
  exit 1
fi

echo "[verify] code quality audit"
bash "$ROOT_DIR/scripts/ops/code_quality_audit.sh"

echo "[verify] language static (manifest lint + wasm ABI, same as CI)"
VH_RUN_ID="${VERIFY_PROJECT_HEALTH_RUN_ID:-verify_project_health_$(date -u +%Y%m%dT%H%M%SZ)}"
MODE=lang_static RUN_ID="$VH_RUN_ID" bash "$ROOT_DIR/scripts/tests/run_daily.sh"

echo "[verify] go test ./..."
go test ./...
echo "[verify] go vet ./..."
go vet ./...
echo "[verify] build hackme-node (trimpath)"
go build -trimpath -ldflags "-s -w" -o /tmp/hackme-node-verify . \
  && rm -f /tmp/hackme-node-verify
echo "[verify] OK (reports/tests/$VH_RUN_ID/ from lang_static)"
