#!/usr/bin/env bash
# Start a local HMS coordinator on 127.0.0.1:18082 (prelaunch / lab).
# For the full disk+seal pilot use: bash scripts/local/ops/hms_local_pilot.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

ADDR="${HMS_COORDINATOR_ADDR:-127.0.0.1:18082}"
DB="${HMS_COORDINATOR_DB:-$ROOT/data/hms_coordinator_lab.db}"
LOG_DIR="${LOG_DIR:-$ROOT/logs/hms_lab}"
mkdir -p "$LOG_DIR" "$(dirname "$DB")"

echo "[hms-up] build hmscoordinator"
go build -trimpath -o "$ROOT/bin/hmscoordinator" ./cmd/hmscoordinator

export HMS_COORDINATOR_ADDR="$ADDR"
export HMS_COORDINATOR_DB="$DB"
export HMS_COORDINATOR_ALLOW_INSECURE="${HMS_COORDINATOR_ALLOW_INSECURE:-1}"
export HMS_EPOCH_SECONDS="${HMS_EPOCH_SECONDS:-120}"
export HMS_FREEZE_AFTER_SEC="${HMS_FREEZE_AFTER_SEC:-90}"
export HMS_SEAL_WINDOW_SEC="${HMS_SEAL_WINDOW_SEC:-60}"

if command -v fuser >/dev/null 2>&1; then
  fuser -k 18082/tcp >/dev/null 2>&1 || true
fi

echo "[hms-up] listening $ADDR db=$DB"
exec "$ROOT/bin/hmscoordinator"
