#!/usr/bin/env bash
# Gate: Hunt async replay queue — enqueue 202 path + drain + load burst.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

echo "[hunt-replay-async-gate] unit tests"
go test -count=1 ./internal/poolfuzz/... -run 'HuntReplayAsync' -timeout 5m

echo "[hunt-replay-async-gate] PASS"
