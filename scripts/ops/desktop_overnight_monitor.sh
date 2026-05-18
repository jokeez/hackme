#!/usr/bin/env bash
# Overnight desktop mining monitor: snapshots until END time, then summary + delta vs baseline.
set -euo pipefail

require_cmd() { command -v "$1" >/dev/null || { echo "[overnight] missing: $1" >&2; exit 1; }; }
require_cmd curl
require_cmd jq
require_cmd python3

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
[[ -f "$DESKTOP_ENV" ]] && set -a && . "$DESKTOP_ENV" && set +a

LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
CANON_BASE="${CANON_BASE:-https://hackme.tech}"
COORD_BASE="${COORD_BASE:-https://hackme.tech/pool/coordinator}"
ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-}"
COORD_TOKEN="${HACKME_POOL_COORDINATOR_TOKEN:-$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)}"
INTERVAL_SEC="${INTERVAL_SEC:-60}"
RUN_ID="${RUN_ID:-overnight_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT/reports/overnight}"
OUT="$OUT_DIR/$RUN_ID"
mkdir -p "$OUT"

# Default: run until tomorrow 08:00 local time.
if [[ -z "${END_EPOCH:-}" ]]; then
  END_EPOCH="$(date -d 'tomorrow 08:00' +%s 2>/dev/null || date -d '+8 hours' +%s)"
fi
start_epoch="$(date +%s)"
if [[ "$END_EPOCH" -le "$start_epoch" ]]; then
  END_EPOCH=$((start_epoch + 8 * 3600))
fi
DURATION_SEC=$((END_EPOCH - start_epoch))

JSONL="$OUT/snapshots.jsonl"
SUMMARY="$OUT/summary.json"
BASELINE="$OUT/baseline.json"
LOG="$OUT/monitor.log"
ln -sfn "$OUT" "$OUT_DIR/CURRENT" 2>/dev/null || true

exec >>"$LOG" 2>&1
echo "[overnight] run_id=$RUN_ID start=$(date -Is) end_epoch=$END_EPOCH duration_sec=$DURATION_SEC interval=${INTERVAL_SEC}s"

snapshot_once() {
  local ts epoch hdr coord_hdr
  ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  epoch="$(date +%s)"
  hdr=()
  [[ -n "$ADMIN_TOKEN" ]] && hdr=(-H "X-Hackme-Admin-Token: $ADMIN_TOKEN")
  coord_hdr=()
  [[ -n "$COORD_TOKEN" ]] && coord_hdr=(-H "X-Hackme-Admin-Token: $COORD_TOKEN")

  local st_lite wallet work worker coord coord_pub
  st_lite="$(curl -fsS --max-time 20 "${hdr[@]}" "$LOCAL_BASE/api/status?lite=1" 2>/dev/null || echo '{}')"
  wallet="$(curl -fsS --max-time 25 "${hdr[@]}" "$LOCAL_BASE/api/wallet?fresh=1" 2>/dev/null || echo '{}')"
  work="$(curl -fsS --max-time 20 "${hdr[@]}" "$LOCAL_BASE/api/work/stats?details=1" 2>/dev/null || echo '{}')"
  worker="$(curl -fsS --max-time 10 "${hdr[@]}" "$LOCAL_BASE/api/worker/status" 2>/dev/null || echo '{}')"
  coord="$(curl -fsS --max-time 25 "${coord_hdr[@]}" "$COORD_BASE/api/work/stats?details=1" 2>/dev/null || echo '{}')"
  coord_pub="$(curl -fsS --max-time 15 "$CANON_BASE/api/status?lite=1" 2>/dev/null || echo '{}')"

  jq -nc \
    --arg ts "$ts" \
    --argjson epoch "$epoch" \
    --argjson st_lite "$st_lite" \
    --argjson wallet "$wallet" \
    --argjson work "$work" \
    --argjson worker "$worker" \
    --argjson coord "$coord" \
    --argjson coord_pub "$coord_pub" \
    '{
      ts:$ts, epoch:$epoch,
      local:{status_lite:$st_lite,wallet:$wallet,work:$work,worker:$worker},
      coordinator:{work:$coord, public_status:$coord_pub}
    }'
}

if [[ ! -f "$BASELINE" ]]; then
  snapshot_once | tee "$BASELINE" >/dev/null
  echo "[overnight] baseline written: $BASELINE"
fi

idx=0
while [[ "$(date +%s)" -lt "$END_EPOCH" ]]; do
  snapshot_once >>"$JSONL"
  idx=$((idx + 1))
  if (( idx % 10 == 0 )); then
  pgrep -f 'workerpoh-opencl' >/dev/null || {
    echo "[overnight] WARN workerpoh missing at $(date -Is); attempting reset"
    bash "$ROOT/scripts/ops/desktop_worker_reset.sh" || true
  }
  fi
  sleep "$INTERVAL_SEC"
done

python3 - "$JSONL" "$BASELINE" "$SUMMARY" "$RUN_ID" "$DURATION_SEC" "$INTERVAL_SEC" <<'PY'
import json, sys
from pathlib import Path

jsonl = Path(sys.argv[1])
baseline_path = Path(sys.argv[2])
summary_path = Path(sys.argv[3])
run_id, duration, interval = sys.argv[4], int(sys.argv[5]), int(sys.argv[6])

def g(obj, *path, default=0):
    cur = obj
    for p in path:
        if not isinstance(cur, dict):
            return default
        cur = cur.get(p)
    if cur is None:
        return default
    return cur

baseline = json.loads(baseline_path.read_text()) if baseline_path.exists() else {}
rows = [json.loads(l) for l in jsonl.read_text().splitlines() if l.strip()]
last = rows[-1] if rows else baseline

def pack(row):
    wk = g(row, "local", "work", "workers", default={}) or {}
    ck = g(row, "coordinator", "work", "workers", default={}) or {}
    return {
        "ts": g(row, "ts", default=""),
        "wallet_address": g(row, "local", "wallet", "address", default=""),
        "wallet_balance_hmc": g(row, "local", "wallet", "balance_hmc", default=0.0),
        "wallet_source": g(row, "local", "wallet", "wallet_source", default=""),
        "canonical_tip_height": g(row, "local", "status_lite", "canonical_tip_height", default=0),
        "coord_total_payout_hmc": g(row, "coordinator", "work", "total_payout_hmc", default=0.0),
        "coord_submitted_items": g(row, "coordinator", "work", "submitted_items", default=0),
        "worker_kapa_pc": ck.get("worker-kapa-pc") or wk.get("worker-kapa-pc") or {},
        "worker_active": ck.get("worker-active") or wk.get("worker-active") or {},
        "desktop_worker_running": g(row, "local", "worker", "running", default=False),
        "desktop_measured_gh_s": g(row, "local", "worker", "measured_hashrate_gh_s", default=0.0),
    }

b, f = pack(baseline), pack(last)
delta = {k: (f[k] - b[k] if isinstance(f[k], (int, float)) and isinstance(b[k], (int, float)) else None) for k in f if k not in ("ts", "wallet_address", "wallet_source", "worker_kapa_pc", "worker_active")}
for wkey in ("worker_kapa_pc", "worker_active"):
    bp, fp = b.get(wkey) or {}, f.get(wkey) or {}
    delta[wkey + "_payout_hmc"] = float(fp.get("payout_hmc") or 0) - float(bp.get("payout_hmc") or 0)
    delta[wkey + "_ranges"] = int(fp.get("accepted_ranges") or 0) - int(bp.get("accepted_ranges") or 0)

summary = {
    "run_id": run_id,
    "status": "DONE" if rows else "EMPTY",
    "duration_sec": duration,
    "interval_sec": interval,
    "snapshots": len(rows),
    "baseline": b,
    "final": f,
    "delta": delta,
}
summary_path.write_text(json.dumps(summary, indent=2))
print(json.dumps(summary, indent=2))
PY

echo "[overnight] done summary=$SUMMARY log=$LOG"
