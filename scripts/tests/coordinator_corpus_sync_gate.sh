#!/usr/bin/env bash
# Gate: coordinator namespace corpus upload + list roundtrip (Dig cross-campaign sync).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

COORD_DB="${COORD_DB:-$(mktemp "${TMPDIR:-/tmp}/hackme-corpus-sync-gate.XXXXXX.db")}"
rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true
export HACKME_COORDINATOR_DB="$COORD_DB"
COORD_PORT="${COORD_PORT:-$((18400 + RANDOM % 800))}"
export HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}"
BASE="http://${HACKME_COORDINATOR_ADDR}"
export HACKME_COORDINATOR_ALLOW_INSECURE=1
export HACKME_COORDINATOR_ADMIN_TOKEN="corpus-sync-gate-admin"
export HACKME_COORDINATOR_WORKER_TOKEN="corpus-sync-gate-worker"

echo "[corpus-sync-gate] unit tests"
go test -count=1 ./cmd/coordinator/ -run TestCorpusNamespaceUploadRoundtrip -timeout=60s

echo "[corpus-sync-gate] start coordinator"
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

NS="gate-bounds-smoke-$(date +%s)"
echo "[corpus-sync-gate] POST namespace corpus"
curl -fsS -X POST "${BASE}/api/fuzz/pool/corpus/namespace" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $HACKME_COORDINATOR_ADMIN_TOKEN" \
  -d "$(jq -nc --arg ns "$NS" \
    '{namespace:$ns,seeds:[{input_u64:42,input_bytes:"AQID",energy:9,edge:3,path:1},{input_u64:99,input_bytes:"",energy:5,edge:1,path:0}]}')" \
  | jq -e '.ok == true and .count == 2' >/dev/null

echo "[corpus-sync-gate] verify via sqlite"
python3 - "$COORD_DB" "$NS" <<'PY'
import sqlite3, sys
db, ns = sys.argv[1], sys.argv[2]
con = sqlite3.connect(db)
n = con.execute("SELECT COUNT(*) FROM fuzz_corpus_namespace WHERE namespace=?", (ns,)).fetchone()[0]
if n != 2:
    raise SystemExit(f"want 2 rows got {n}")
u64 = con.execute("SELECT input_u64 FROM fuzz_corpus_namespace WHERE namespace=? AND input_u64=42", (ns,)).fetchone()
if not u64:
    raise SystemExit("missing seed input_u64=42")
PY

pass "coordinator_corpus_sync_gate PASS"
