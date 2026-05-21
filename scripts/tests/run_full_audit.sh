#!/usr/bin/env bash
# Full local audit: Go tests + optional coordinator load + Playwright E2E.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

echo "== Go test ./... =="
go test ./... -count=1

echo "== Difficulty + payout focused =="
go test ./cmd/coordinator/... ./internal/chain/... -count=1 -run 'TestPayout|TestPool|TestMaybeRetarget|TestRetarget'

if curl -fsS "${COORD:-http://127.0.0.1:8081}/health" >/dev/null 2>&1; then
  echo "== Mock miners load =="
  WORKERS="${WORKERS:-12}" DURATION_SEC="${DURATION_SEC:-30}" bash scripts/tests/mock_miners_load.sh
else
  echo "[skip] mock miners — coordinator not on ${COORD:-http://127.0.0.1:8081}"
fi

if command -v npm >/dev/null 2>&1; then
  echo "== Playwright E2E =="
  bash scripts/tests/run_ui_e2e.sh
else
  echo "[skip] Playwright — npm not installed"
fi

echo "== Full audit PASS =="
