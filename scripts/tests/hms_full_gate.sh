#!/usr/bin/env bash
# Full HMS gate: unit tests + market API + local disks (if node up).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[hms-full-gate] prelaunch (red team + payouts + market)"
bash scripts/ops/hms_prelaunch_gate.sh

if curl -fsS --max-time 2 http://127.0.0.1:8080/api/local/disks >/dev/null 2>&1; then
  echo "[hms-full-gate] desktop disks API"
  curl -fsS http://127.0.0.1:8080/api/local/disks | jq -e '.status == "ok" and (.disks | length) >= 0' >/dev/null
fi

if curl -fsS --max-time 2 http://127.0.0.1:18082/api/pool/stats >/dev/null 2>&1; then
  echo "[hms-full-gate] coordinator stats"
  curl -fsS http://127.0.0.1:18082/api/pool/stats | jq -e '.status == "ok"' >/dev/null
  curl -fsS http://127.0.0.1:18082/api/market/stats | jq -e '.status == "ok"' >/dev/null
fi

echo "[hms-full-gate] OK"
