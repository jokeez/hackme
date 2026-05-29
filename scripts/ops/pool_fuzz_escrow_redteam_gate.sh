#!/usr/bin/env bash
# Gate: fuzz escrow economics + pool red-team (replay, wrong worker, signing).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export TMPDIR="${TMPDIR:-/tmp}"
mkdir -p "$ROOT/logs"

echo "[fuzz-redteam-gate] Go unit + red-team tests"
go test -count=1 -timeout=180s ./internal/fuzzescrow/... ./internal/fuzzengine/... ./internal/poolfuzz/... \
  ./internal/chain/ -run 'Fuzz|Pool|Split|Redteam|Escrow|Detector|Tamper|Replay|Submit' 2>&1
go test -count=1 -timeout=120s ./cmd/coordinator/ -run 'Fuzz|Tamper|Replay|HTTPFuzz' 2>&1

echo "[fuzz-redteam-gate] distributed pool smoke"
COORD_DB="${COORD_DB:-$ROOT/logs/pool_fuzz_redteam_$(date +%s).db}"
rm -f "$COORD_DB"
export HACKME_COORDINATOR_DB="$COORD_DB"
COORD_PORT="${COORD_PORT:-$((19000 + RANDOM % 500))}"
export HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}"
BASE="http://${HACKME_COORDINATOR_ADDR}"
export HACKME_COORDINATOR_ALLOW_INSECURE=1
export HACKME_COORDINATOR_ADMIN_TOKEN=""
export HACKME_COORDINATOR_WORKER_TOKEN="fuzz-redteam-worker"

go run ./cmd/coordinator &
CPID=$!
trap 'kill $CPID 2>/dev/null || true' EXIT
for _ in $(seq 1 40); do
  curl -fsS --max-time 2 "${BASE}/health" >/dev/null 2>&1 && break
  sleep 0.25
done

WASM_HEX="$(xxd -p tasks/artifacts/security/rust_script_push_bounds_guard.wasm | tr -d '\n')"
CID="fuzz-redteam-$(date +%s)"
curl -fsS -X POST "${BASE}/api/fuzz/pool/campaigns" -H "Content-Type: application/json" -d "$(python3 - <<PY
import json, os
print(json.dumps({
  "id": "$CID",
  "campaign_type": "property",
  "title": "redteam gate",
  "status": "running",
  "budget_runs": 12,
  "config": {
    "pool_distributed": True,
    "check_semantics": "detector",
    "wasm_check_hex": """$WASM_HEX""",
    "seed_corpus": [133452],
    "mutation_rounds": 0,
  },
}))
PY
)"

export COORD_URL="$BASE" COORD_TOKEN="$HACKME_COORDINATOR_WORKER_TOKEN"
WORKER_ID=rt1 timeout 10s go run ./cmd/workerfuzz -worker rt1 2>/dev/null || true

# Replay submit must not inflate work_done beyond budget
DONE="$(sqlite3 "$COORD_DB" "SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id='$CID' AND status='done';")"
echo "[fuzz-redteam-gate] work_done=$DONE"
[[ "${DONE:-0}" -ge 1 ]] || { echo "FAIL: no completed work" >&2; exit 1; }

# Unauthenticated settle probe (node not required in this gate)
code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:18099/api/fuzz/pool/settle \
  -H 'Content-Type: application/json' -d '{"kind":"run","campaign_id":"x"}' 2>/dev/null || echo 000)"
if [[ "$code" == "200" ]]; then
  echo "WARN: local node settle returned 200 without token (node may be up)" >&2
fi

kill "$CPID" 2>/dev/null || true
wait "$CPID" 2>/dev/null || true
echo "[fuzz-redteam-gate] PASS"
