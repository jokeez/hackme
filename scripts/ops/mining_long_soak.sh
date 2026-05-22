#!/usr/bin/env bash
set -euo pipefail

# Long-running mining soak monitor.
# Captures local/canonical/coordinator snapshots and writes delta summary.
#
# Usage:
#   DURATION_SEC=1800 INTERVAL_SEC=30 \
#   LOCAL_BASE=http://127.0.0.1:8080 \
#   CANON_BASE=https://hackme.tech \
#   COORD_BASE=http://hackme-vps:18081 \
#   bash scripts/ops/mining_long_soak.sh

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[mining-soak] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq
require_cmd python3

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RUN_ID="${RUN_ID:-mining_soak_$(date -u +%Y%m%dT%H%M%SZ)}"
DURATION_SEC="${DURATION_SEC:-1200}"
INTERVAL_SEC="${INTERVAL_SEC:-20}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
CANON_BASE="${CANON_BASE:-https://hackme.tech}"
COORD_BASE="${COORD_BASE:-https://hackme.tech/pool/coordinator}"

OUT="$OUT_DIR/$RUN_ID/mining_soak"
mkdir -p "$OUT"
JSONL="$OUT/snapshots.jsonl"
SUMMARY="$OUT/summary.json"

start_epoch="$(date +%s)"
end_epoch=$((start_epoch + DURATION_SEC))
idx=0

echo "[mining-soak] run_id=$RUN_ID duration=${DURATION_SEC}s interval=${INTERVAL_SEC}s"
echo "[mining-soak] local=$LOCAL_BASE canon=$CANON_BASE coord=$COORD_BASE"

snapshot_once() {
  local ts epoch
  ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  epoch="$(date +%s)"
  local local_status local_wallet local_work canon_status canon_blocks coord_work
  local_status="$(curl -fsS --max-time 12 "$LOCAL_BASE/api/status" || echo '{}')"
  local_wallet="$(curl -fsS --max-time 12 "$LOCAL_BASE/api/wallet" || echo '{}')"
  local_work="$(curl -fsS --max-time 12 "$LOCAL_BASE/api/work/stats?details=1" || echo '{}')"
  canon_status="$(curl -fsS --max-time 12 "$CANON_BASE/api/status" || echo '{}')"
  canon_blocks="$(curl -fsS --max-time 12 "$CANON_BASE/api/reports/blocks" || echo '{}')"
  coord_work="$(curl -fsS --max-time 12 "$COORD_BASE/api/work/stats?details=1" || echo '{}')"

  jq -nc \
    --arg ts "$ts" \
    --argjson epoch "$epoch" \
    --argjson local_status "$local_status" \
    --argjson local_wallet "$local_wallet" \
    --argjson local_work "$local_work" \
    --argjson canon_status "$canon_status" \
    --argjson canon_blocks "$canon_blocks" \
    --argjson coord_work "$coord_work" \
    '{
      ts:$ts,
      epoch:$epoch,
      local:{status:$local_status,wallet:$local_wallet,work:$local_work},
      canonical:{status:$canon_status,blocks:$canon_blocks},
      coordinator:{work:$coord_work}
    }'
}

while [[ "$(date +%s)" -lt "$end_epoch" ]]; do
  snapshot_once >>"$JSONL"
  idx=$((idx + 1))
  sleep "$INTERVAL_SEC"
done

python3 - "$JSONL" "$SUMMARY" "$RUN_ID" "$DURATION_SEC" "$INTERVAL_SEC" "$LOCAL_BASE" "$CANON_BASE" "$COORD_BASE" <<'PY'
import json
import sys
from pathlib import Path

jsonl_path = Path(sys.argv[1])
summary_path = Path(sys.argv[2])
run_id = sys.argv[3]
duration = int(sys.argv[4])
interval = int(sys.argv[5])
local_base, canon_base, coord_base = sys.argv[6], sys.argv[7], sys.argv[8]

rows = []
for line in jsonl_path.read_text().splitlines():
    line = line.strip()
    if not line:
        continue
    rows.append(json.loads(line))

if not rows:
    summary = {
        "run_id": run_id,
        "status": "EMPTY",
        "error": "no snapshots captured",
    }
    summary_path.write_text(json.dumps(summary, indent=2))
    print(json.dumps(summary))
    sys.exit(0)

first, last = rows[0], rows[-1]

def g(obj, *path, default=0):
    cur = obj
    for p in path:
        if not isinstance(cur, dict):
            return default
        cur = cur.get(p)
    if cur is None:
        return default
    return cur

f_h = g(first, "canonical", "status", "tip_height", default=0)
l_h = g(last, "canonical", "status", "tip_height", default=0)
f_bal = g(first, "local", "wallet", "balance_units", default=0)
l_bal = g(last, "local", "wallet", "balance_units", default=0)
f_nonce = g(first, "local", "wallet", "next_nonce", default=0)
l_nonce = g(last, "local", "wallet", "next_nonce", default=0)
f_ar = g(first, "local", "work", "workers", "worker-active", "accepted_ranges", default=0)
l_ar = g(last, "local", "work", "workers", "worker-active", "accepted_ranges", default=0)
f_aa = g(first, "local", "work", "workers", "worker-active", "accepted_attempts", default=0)
l_aa = g(last, "local", "work", "workers", "worker-active", "accepted_attempts", default=0)
f_pay = g(first, "local", "work", "workers", "worker-active", "payout_hmc", default=0.0)
l_pay = g(last, "local", "work", "workers", "worker-active", "payout_hmc", default=0.0)

summary = {
    "run_id": run_id,
    "status": "DONE",
    "duration_sec": duration,
    "interval_sec": interval,
    "snapshots": len(rows),
    "endpoints": {
        "local_base": local_base,
        "canonical_base": canon_base,
        "coordinator_base": coord_base,
    },
    "baseline": {
        "tip_height": f_h,
        "wallet_balance_units": f_bal,
        "wallet_next_nonce": f_nonce,
        "worker_active_accepted_ranges": f_ar,
        "worker_active_accepted_attempts": f_aa,
        "worker_active_payout_hmc": f_pay,
    },
    "final": {
        "tip_height": l_h,
        "wallet_balance_units": l_bal,
        "wallet_next_nonce": l_nonce,
        "worker_active_accepted_ranges": l_ar,
        "worker_active_accepted_attempts": l_aa,
        "worker_active_payout_hmc": l_pay,
    },
    "delta": {
        "tip_height": l_h - f_h,
        "wallet_balance_units": l_bal - f_bal,
        "wallet_next_nonce": l_nonce - f_nonce,
        "worker_active_accepted_ranges": l_ar - f_ar,
        "worker_active_accepted_attempts": l_aa - f_aa,
        "worker_active_payout_hmc": l_pay - f_pay,
    },
}
summary_path.write_text(json.dumps(summary, indent=2))
print(json.dumps(summary))
PY

echo "[mining-soak] done: $OUT"
echo "[mining-soak] summary: $SUMMARY"
