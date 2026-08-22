#!/usr/bin/env bash
# Local pack product gate: unit/demo + in-process E2E (wizard→autorun→report/gate explain).
# No push, no site deploy, no VPS.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
PACK="${1:-secrets}"

echo "[pack-e2e] 1/4 packs CLI + unit tests"
go test -count=1 ./internal/fuzzingcli/ ./cmd/fuzzingclient/ -timeout=90s
go run ./cmd/fuzzingclient packs >/tmp/hackme-packs.json
jq -e '.ok == true and (.packs|length) >= 3' /tmp/hackme-packs.json >/dev/null

echo "[pack-e2e] 2/4 sandbox demo pack=$PACK"
go run ./tools/pack_demo -pack "$PACK" -runs "${RUNS:-48}" -workers 2 | tee /tmp/hackme-pack-demo.txt
grep -q 'explain:' /tmp/hackme-pack-demo.txt

echo "[pack-e2e] 3/4 in-process security-audit → report/gate"
go test -count=1 -timeout=120s -run TestPackSecretsE2EAuditReportExplain .

echo "[pack-e2e] 4/4 native/ASAN honesty check"
bash "$ROOT/scripts/ops/check_native_asan_story.sh"

echo ""
echo "[pack-e2e] PASS — customer path ready locally"
echo "  hackme-fuzzing packs"
echo "  hackme-fuzzing wizard --pack $PACK --package audit   # needs local node + escrow"
echo "  (push/deploy still deferred to exchange window)"
