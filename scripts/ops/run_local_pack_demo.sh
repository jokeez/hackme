#!/usr/bin/env bash
# Local pack product demo — no push, no site deploy, no VPS.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
PACK="${1:-secrets}"
RUNS="${RUNS:-64}"

echo "[pack-demo] list packs via CLI"
go run ./cmd/fuzzingclient packs 2>/dev/null | head -5 || true
go run ./cmd/fuzzingclient packs 2>&1 | tail -20

echo ""
echo "[pack-demo] dry-run wizard payload --pack=$PACK"
go test -count=1 ./cmd/fuzzingclient/ -run TestWizardDryRunPackSecrets -v 2>&1 | tail -15 || true

echo ""
echo "[pack-demo] sandbox run pack=$PACK runs=$RUNS"
go run ./tools/pack_demo/ -pack "$PACK" -runs "$RUNS" -workers 2

echo ""
echo "[pack-demo] OK — example customer flow:"
echo "  hackme-fuzzing packs"
echo "  hackme-fuzzing wizard --pack $PACK --package audit"
echo "  (local node + escrow; miners hybrid unchanged)"
