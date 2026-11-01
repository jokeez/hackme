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
export HACKME_COORDINATOR_ALLOW_INSECURE=0
export HACKME_COORDINATOR_ADMIN_TOKEN="hunt-harness-gate-admin"
export HACKME_COORDINATOR_WORKER_TOKEN="hunt-harness-gate-worker"
export HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN="hunt-harness-gate-publish"

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
# Must be hex — PutHarnessArtifact rejects non-hex ids (path-safe cache keys).
HASH="$(sha256sum "$WASM" | awk '{print $1}')"
B64="$(base64 -w0 "$WASM")"

echo "[hunt-harness-gate] POST harness publish (admin JSON)"
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

# Customer Orders path: scoped publish token + octet-stream (Bobby pilot gap).
PUB_HASH="$(printf 'publish-octet-%s' "$HASH" | sha256sum | awk '{print $1}')"
echo "[hunt-harness-gate] POST harness publish (orders publish token / octet-stream)"
code="$(curl -sS -o /tmp/hunt-harness-pub.json -w '%{http_code}' -X POST "${BASE}/api/fuzz/pool/hunt/harness" \
  -H "Content-Type: application/octet-stream" \
  -H "X-Hackme-Harness-Hash: $PUB_HASH" \
  -H "X-Hackme-Source-Rel: customer/pilot" \
  -H "X-Hackme-Admin-Token: $HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN" \
  --data-binary @"$WASM")"
if [[ "$code" != "200" ]]; then
  fail "orders publish harness upload want 200 got $code body=$(head -c 200 /tmp/hunt-harness-pub.json)"
fi
jq -e '.ok == true' /tmp/hunt-harness-pub.json >/dev/null

TMP2="$(mktemp)"
curl -fsS -H "Authorization: Bearer $HACKME_COORDINATOR_WORKER_TOKEN" \
  "${BASE}/api/fuzz/pool/hunt/harness/${PUB_HASH}" -o "$TMP2"
if ! cmp -s "$WASM" "$TMP2"; then
  fail "worker fetch after publish-token upload mismatch"
fi
rm -f "$TMP2" /tmp/hunt-harness-pub.json

# Negative: bare worker token must not upload.
neg="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${BASE}/api/fuzz/pool/hunt/harness" \
  -H "Content-Type: application/octet-stream" \
  -H "X-Hackme-Harness-Hash: $(printf neg | sha256sum | awk '{print $1}')" \
  -H "X-Hackme-Admin-Token: $HACKME_COORDINATOR_WORKER_TOKEN" \
  --data-binary @"$WASM")"
if [[ "$neg" != "401" ]]; then
  fail "worker harness upload want 401 got $neg"
fi

pass "hunt_harness_publish_gate PASS"
