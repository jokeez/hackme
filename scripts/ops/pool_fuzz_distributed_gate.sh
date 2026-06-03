#!/usr/bin/env bash
# Gate: distributed pool fuzz (coordinator claim/submit + detector semantics).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[pool-fuzz-gate] unit tests"
go test -count=1 ./internal/fuzzengine/... ./internal/poolfuzz/...

COORD_DB="${COORD_DB:-$(mktemp "${TMPDIR:-/tmp}/hackme-pool-fuzz-gate.XXXXXX.db")}"
rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true
export HACKME_COORDINATOR_DB="$COORD_DB"
COORD_PORT="${COORD_PORT:-$((18100 + RANDOM % 800))}"
export HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}"
BASE="http://${HACKME_COORDINATOR_ADDR}"
export HACKME_COORDINATOR_ALLOW_INSECURE=1
export HACKME_COORDINATOR_ADMIN_TOKEN=""
export HACKME_COORDINATOR_WORKER_TOKEN="pool-fuzz-gate-worker"

echo "[pool-fuzz-gate] start coordinator"
go run ./cmd/coordinator &
CPID=$!
cleanup_gate() {
  kill "$CPID" 2>/dev/null || true
  rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true
}
trap cleanup_gate EXIT
for _ in $(seq 1 30); do
  if curl -fsS --max-time 2 "${BASE}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.3
done

WASM_HEX="$(xxd -p tasks/artifacts/security/rust_script_push_bounds_guard.wasm | tr -d '\n')"
export WASM_HEX
CID="pool-fuzz-gate-$(date +%s)"
export CID

echo "[pool-fuzz-gate] register campaign $CID"
curl -fsS -X POST "${BASE}/api/fuzz/pool/campaigns" \
  -H "Content-Type: application/json" \
  -d "$(python3 - <<'PY'
import json, os
print(json.dumps({
  "id": os.environ["CID"],
  "campaign_type": "property",
  "title": "pool fuzz gate",
  "status": "running",
  "budget_runs": 24,
  "budget_seconds": 120,
  "config": {
    "pool_distributed": True,
    "check_semantics": "detector",
    "wasm_check_hex": os.environ["WASM_HEX"],
    "seed_corpus": [133452, 133452],
    "mutation_rounds": 0,
  },
}))
PY
)"

echo "[pool-fuzz-gate] run workerfuzz workers"
export COORD_URL="$BASE"
export COORD_TOKEN="$HACKME_COORDINATOR_WORKER_TOKEN"
for w in wf1 wf2; do
  WORKER_ID="$w" timeout 12s go run ./cmd/workerfuzz -worker "$w" 2>/dev/null || true
done

ST="$(curl -fsS "${BASE}/api/fuzz/pool/stats")"
echo "$ST" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('work_done',0)>=8, d; print('work_done', d['work_done'])"

kill "$CPID" 2>/dev/null || true
wait "$CPID" 2>/dev/null || true
sleep 0.5

FINDINGS="$(sqlite3 "$COORD_DB" "SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id='$CID';")"
VIOLATIONS="$(sqlite3 "$COORD_DB" "SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id='$CID' AND status='done' AND result_ok=0;")"
echo "[pool-fuzz-gate] findings=$FINDINGS violations_done=$VIOLATIONS"
if [[ "${FINDINGS:-0}" -lt 1 && "${VIOLATIONS:-0}" -lt 1 ]]; then
  echo "[pool-fuzz-gate] FAIL: expected detector findings or violation work items" >&2
  exit 1
fi

echo "[pool-fuzz-gate] PASS"
trap - EXIT
