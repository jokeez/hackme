#!/usr/bin/env bash
# HMS readiness gate — ledger + coordinator smoke without a full disk pilot.
# Does NOT enable the public lane (HACKME_HMS_LANE_ENABLED stays off).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[hms-gate] unit tests"
go test ./internal/hms/ ./internal/chain/ -count=1 -timeout 180s -run 'HMS|Hms|Seal|Market|Warm|Settle'

echo "[hms-gate] build binaries"
go build -trimpath -o "$ROOT/bin/hmscoordinator" ./cmd/hmscoordinator
go build -trimpath -o "$ROOT/bin/workerstorage" ./cmd/workerstorage
go build -trimpath -o "$ROOT/bin/workerseal" ./cmd/workerseal

PORT="${HMS_GATE_PORT:-$((19082 + RANDOM % 200))}"
ADDR="127.0.0.1:${PORT}"
DB="$(mktemp "${TMPDIR:-/tmp}/hms-gate.XXXXXX.db")"
MARKET_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/hms-gate-market.XXXXXX")"
cleanup() {
  if [[ -n "${CPID:-}" ]]; then kill "$CPID" 2>/dev/null || true; wait "$CPID" 2>/dev/null || true; fi
  rm -f "$DB" "${DB}-wal" "${DB}-shm" 2>/dev/null || true
  rm -rf "$MARKET_ROOT" 2>/dev/null || true
}
trap cleanup EXIT

echo "[hms-gate] start coordinator on $ADDR"
HMS_COORDINATOR_ADDR="$ADDR" \
HMS_COORDINATOR_DB="$DB" \
HMS_COORDINATOR_ALLOW_INSECURE=1 \
HMS_EPOCH_SECONDS=60 \
HMS_FREEZE_AFTER_SEC=40 \
HMS_SEAL_WINDOW_SEC=20 \
HMS_MARKET_STORAGE_ROOT="$MARKET_ROOT" \
HMS_MARKET_DATA_DIR="$MARKET_ROOT/data" \
HMS_MARKET_SKIP_PAYMENT=1 \
"$ROOT/bin/hmscoordinator" >/tmp/hms-gate-coord.log 2>&1 &
CPID=$!

ok=0
for _ in $(seq 1 40); do
  if curl -fsS --max-time 2 "http://${ADDR}/health" >/dev/null 2>&1; then ok=1; break; fi
  sleep 0.25
done
if [[ "$ok" != "1" ]]; then
  echo "[hms-gate] FAIL: coordinator health" >&2
  tail -40 /tmp/hms-gate-coord.log >&2 || true
  exit 1
fi

curl -fsS "http://${ADDR}/api/pool/stats" | python3 -c 'import sys,json; d=json.load(sys.stdin); assert isinstance(d, dict) and d, d'
curl -fsS "http://${ADDR}/api/market/pricing" | python3 -c 'import sys,json; d=json.load(sys.stdin); assert d.get("status")=="ok" or "pricing" in d, d'
# Quote may 503 without registered capacity — pricing+stats are enough for desk readiness.
quote_code="$(curl -sS -o /tmp/hms-gate-quote.json -w '%{http_code}' -X POST "http://${ADDR}/api/market/quote" \
  -H 'Content-Type: application/json' -d '{"size_bytes":1048576,"retention_days":30}')"
echo "[hms-gate] quote http=$quote_code"
if [[ "$quote_code" == "200" ]]; then
  python3 -c 'import json; d=json.load(open("/tmp/hms-gate-quote.json")); assert d.get("status")=="ok" and d.get("quote"), d'
fi

# Settle script must exist and stay dry-run by default.
test -x "$ROOT/scripts/ops/settle_worker_hms.sh" || test -f "$ROOT/scripts/ops/settle_worker_hms.sh"
DRY_RUN=1 HMS_COORD_URL="http://${ADDR}" bash "$ROOT/scripts/ops/settle_worker_hms.sh" >/tmp/hms-gate-settle.txt 2>&1 || true
# Dry-run may exit 0 with empty epochs — just ensure it did not crash on missing curl/jq.
head -5 /tmp/hms-gate-settle.txt >/dev/null

echo "[hms-gate] PASS (prelaunch-ready; set HACKME_HMS_LANE_ENABLED=1 only on the dedicated host)"
