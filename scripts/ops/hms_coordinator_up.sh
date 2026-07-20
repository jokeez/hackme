#!/usr/bin/env bash
# Local HMS lane coordinator (Heavy VPS #2 stand-in on loopback).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HMS_COORDINATOR_ADDR="${HMS_COORDINATOR_ADDR:-127.0.0.1:18082}"
export HMS_COORDINATOR_DB="${HMS_COORDINATOR_DB:-$ROOT/data/hms_coordinator.db}"
export HMS_COORDINATOR_ALLOW_INSECURE="${HMS_COORDINATOR_ALLOW_INSECURE:-0}"
mkdir -p "$(dirname "$HMS_COORDINATOR_DB")"
if [[ -f "$ROOT/.secrets/hackme_coordinator_worker_token" ]]; then
  export HMS_COORDINATOR_WORKER_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_worker_token")"
fi
echo "[hms-up] build hmscoordinator"
go build -trimpath -o "$ROOT/bin/hmscoordinator" ./cmd/hmscoordinator
echo "[hms-up] listening $HMS_COORDINATOR_ADDR db=$HMS_COORDINATOR_DB"
exec "$ROOT/bin/hmscoordinator"
