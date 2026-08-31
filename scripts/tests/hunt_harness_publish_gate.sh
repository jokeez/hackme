#!/usr/bin/env bash
# Gate: Hunt harness publish + coordinator fetch roundtrip.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

if ! command -v clang >/dev/null 2>&1; then
  echo "[hunt-harness-gate] SKIP: clang not installed" >&2
  exit 0
fi

echo "[hunt-harness-gate] unit tests"
go test -count=1 ./internal/hunt/... -run 'HarnessArtifact|HarnessFetch' -timeout=30s

COORD_DB="${COORD_DB:-$(mktemp "${TMPDIR:-/tmp}/hackme-hunt-harness-gate.XXXXXX.db")}"
rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true
export HACKME_COORDINATOR_DB="$COORD_DB"
COORD_PORT="${COORD_PORT:-$((18300 + RANDOM % 800))}"
export HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}"
BASE="http://${HACKME_COORDINATOR_ADDR}"
export HACKME_COORDINATOR_ALLOW_INSECURE=1
export HACKME_COORDINATOR_ADMIN_TOKEN="hunt-harness-gate-admin"
export HACKME_COORDINATOR_WORKER_TOKEN="hunt-harness-gate-worker"

echo "[hunt-harness-gate] start coordinator"
go run ./cmd/coordinator &
CPID=$!
cleanup_gate() {
  kill "$CPID" 2>/dev/null || true
  rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true
}
trap cleanup_gate EXIT
for _ in $(seq 1 40); do
  if curl -fsS --max-time 2 "${BASE}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.3
done

bash "$ROOT/scripts/ops/build_scan_smoke_guards.sh" >/dev/null
WASM="$ROOT/tasks/artifacts/security/rust_bounds_smoke_guard.wasm"
HASH="gate-smoke-$(date +%s)"
B64="$(base64 -w0 "$WASM")"

echo "[hunt-harness-gate] POST harness publish"
curl -fsS -X POST "${BASE}/api/fuzz/pool/hunt/harness" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $HACKME_COORDINATOR_ADMIN_TOKEN" \
  -d "$(jq -nc --arg h "$HASH" --arg b "$B64" '{harness_hash:$h, source_rel:"bounds_smoke", binary_b64:$b}')" \
  | jq -e '.ok == true' >/dev/null

TMP="$(mktemp)"
curl -fsS -H "Authorization: Bearer $HACKME_COORDINATOR_WORKER_TOKEN" \
  "${BASE}/api/fuzz/pool/hunt/harness/${HASH}" -o "$TMP"
if ! cmp -s "$WASM" "$TMP"; then
  fail "coordinator harness fetch mismatch"
fi
rm -f "$TMP"

pass "hunt_harness_publish_gate PASS"
