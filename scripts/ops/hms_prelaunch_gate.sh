#!/usr/bin/env bash
# HMS prelaunch readiness gate — unit tests + market + loopback pilot + settle dry-run (local only).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[hms-prelaunch] build binaries"
mkdir -p bin
go build -trimpath -o bin/hmscoordinator ./cmd/hmscoordinator
go build -trimpath -o bin/workerstorage ./cmd/workerstorage
go build -trimpath -o bin/workerseal ./cmd/workerseal

echo "[hms-prelaunch] unit tests ./internal/hms + chain HMS"
go test ./internal/hms/ -count=1 -timeout 180s
go test ./internal/chain/ -count=1 -timeout 120s -run 'HMS|Hms|PayHMS'

echo "[hms-prelaunch] economics policy marker"
go test ./internal/hms/ -count=1 -run 'LaneEconomicsPolicyVersion|SealEpochBudgetUnits|SplitLaneBudget' -v

echo "[hms-prelaunch] settlement script present"
test -x scripts/ops/settle_worker_hms.sh
test -x scripts/ops/hms_epoch_settle.sh
bash -n scripts/ops/settle_worker_hms.sh
bash -n scripts/ops/hms_epoch_settle.sh

echo "[hms-prelaunch] market gate"
bash scripts/tests/hms_market_gate.sh

echo "[hms-prelaunch] market redteam"
bash scripts/tests/hms_market_redteam.sh

echo "[hms-prelaunch] market integrity"
bash scripts/tests/hms_market_integrity.sh

echo "[hms-prelaunch] loopback pilot (seal + restore + settle dry-run)"
bash scripts/tests/hms_loopback_pilot.sh

echo "[hms-prelaunch] OK — ready for internal pilot (not public launch)"
