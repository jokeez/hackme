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
WORKERS_CSV="${WORKERS:-worker-kapa-pc,worker-vps-msk-01,vps-canary-01,worker-vps-62-01}"
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
DIFF_JSONL="$OUT/difficulty.jsonl"
SUMMARY="$OUT/summary.json"
BASELINE="$OUT/baseline.json"
LOG="$OUT/monitor.log"
DIFF_PY="$ROOT/scripts/ops/mining_difficulty_trace.py"
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
  metrics="$(curl -fsS --max-time 15 "${hdr[@]}" "$LOCAL_BASE/api/metrics" 2>/dev/null || echo '{}')"
  canon_wallet='{}'
  wallet_addr="$(echo "$wallet" | jq -r '.address // empty' 2>/dev/null || true)"
  [[ -z "$wallet_addr" ]] && wallet_addr="HMC-91fe007e4036c602"
  canon_wallet="$(curl -fsS --max-time 20 "$CANON_BASE/api/address/$wallet_addr" 2>/dev/null || echo '{}')"

  jq -nc \
    --arg ts "$ts" \
    --argjson epoch "$epoch" \
    --argjson st_lite "$st_lite" \
    --argjson wallet "$wallet" \
    --argjson work "$work" \
    --argjson worker "$worker" \
    --argjson metrics "$metrics" \
    --argjson coord "$coord" \
    --argjson coord_pub "$coord_pub" \
    --argjson canon_wallet "$canon_wallet" \
    '{
      ts:$ts, epoch:$epoch,
      local:{status_lite:$st_lite,wallet:$wallet,work:$work,worker:$worker,metrics:$metrics},
      coordinator:{work:$coord, public_status:$coord_pub},
      canonical:{wallet:$canon_wallet}
    }'
}

append_difficulty_sample() {
  local snap="$1"
  echo "$snap" | jq -c '{
    ts:.ts, epoch:.epoch,
    target_mod:(.coordinator.work.target_mod // .local.work.target_mod // 0),
    target_mod_updated_unix:(.coordinator.work.target_mod_updated_unix // .local.work.target_mod_updated_unix // 0),
    pool_retarget_enabled:(.coordinator.work.pool_retarget_enabled // false),
    reward_per_m:(.coordinator.work.reward_per_m // .local.work.reward_per_m // 0),
    found_bonus_hmc:(.coordinator.work.found_bonus_hmc // .local.work.found_bonus_hmc // 0),
    scheduler_mode:(.coordinator.work.scheduler_mode // ""),
    orders_active:(.coordinator.work.orders_active // false),
    pool_target_mod_status:(.local.status_lite.pool_target_mod // 0),
    mining_target_mod_metrics:(.local.metrics.mining_target_mod // 0),
    pool_global_hashrate_th_s:(.local.status_lite.pool_global_hashrate_th_s // 0),
    accepted_attempts:(.coordinator.work.accepted_attempts // .local.work.accepted_attempts // 0),
    total_payout_hmc:(.coordinator.work.total_payout_hmc // .local.work.total_payout_hmc // 0)
  }' >>"$DIFF_JSONL" 2>/dev/null || true
}

if [[ ! -f "$BASELINE" ]]; then
  snap="$(snapshot_once)"
  echo "$snap" | tee "$BASELINE" >/dev/null
  append_difficulty_sample "$snap"
  echo "[overnight] baseline written: $BASELINE"
elif [[ ! -f "$DIFF_JSONL" ]] || [[ ! -s "$DIFF_JSONL" ]]; then
  append_difficulty_sample "$(cat "$BASELINE")"
fi

idx=0
while [[ "$(date +%s)" -lt "$END_EPOCH" ]]; do
  snap="$(snapshot_once)"
  echo "$snap" >>"$JSONL"
  append_difficulty_sample "$snap"
  idx=$((idx + 1))
  if (( idx % 10 == 0 )); then
  pgrep -f 'workerpoh-cuda|workerpoh-opencl|workerpoh ' >/dev/null || {
    echo "[overnight] WARN workerpoh missing at $(date -Is); attempting reset"
    bash "$ROOT/scripts/ops/desktop_worker_reset.sh" || true
  }
  fi
  sleep "$INTERVAL_SEC"
done

python3 - "$JSONL" "$BASELINE" "$SUMMARY" "$RUN_ID" "$DURATION_SEC" "$INTERVAL_SEC" "$WORKERS_CSV" <<'PY'
import json, sys
from pathlib import Path

jsonl = Path(sys.argv[1])
baseline_path = Path(sys.argv[2])
summary_path = Path(sys.argv[3])
run_id, duration, interval = sys.argv[4], int(sys.argv[5]), int(sys.argv[6])
worker_ids = [w.strip() for w in sys.argv[7].split(",") if w.strip()]

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
    if not isinstance(wk, dict):
        wk = {}
    if not isinstance(ck, dict):
        ck = {}
    workers = {}
    for wid in worker_ids:
        workers[wid] = ck.get(wid) or wk.get(wid) or {}
    canon_units = g(row, "canonical", "wallet", "balance_units", default=0)
    return {
        "ts": g(row, "ts", default=""),
        "wallet_address": g(row, "local", "wallet", "address", default=""),
        "wallet_balance_hmc": g(row, "local", "wallet", "balance_hmc", default=0.0),
        "canonical_balance_hmc": float(canon_units or 0) / 1e8,
        "unpaid_accrual_hmc": g(row, "local", "wallet", "unpaid_worker_accrual_hmc", default=0.0),
        "wallet_source": g(row, "local", "wallet", "wallet_source", default=""),
        "canonical_tip_height": g(row, "local", "status_lite", "canonical_tip_height", default=0),
        "coord_total_payout_hmc": g(row, "coordinator", "work", "total_payout_hmc", default=0.0),
        "coord_submitted_items": g(row, "coordinator", "work", "submitted_items", default=0),
        "coord_accepted_attempts": g(row, "coordinator", "work", "accepted_attempts", default=0),
        "target_mod": int(g(row, "coordinator", "work", "target_mod", default=0) or g(row, "local", "work", "target_mod", default=0) or 0),
        "reward_per_m": float(g(row, "coordinator", "work", "reward_per_m", default=0) or g(row, "local", "work", "reward_per_m", default=0) or 0),
        "pool_retarget_enabled": bool(g(row, "coordinator", "work", "pool_retarget_enabled", default=False)),
        "workers": workers,
        "desktop_worker_running": g(row, "local", "worker", "running", default=False),
        "desktop_measured_gh_s": g(row, "local", "worker", "measured_hashrate_gh_s", default=0.0),
    }

b, f = pack(baseline), pack(last)
skip = {"ts", "wallet_address", "wallet_source", "workers"}
delta = {
    k: (f[k] - b[k] if isinstance(f[k], (int, float)) and isinstance(b[k], (int, float)) else None)
    for k in f if k not in skip
}
for wid in worker_ids:
    bp, fp = (b.get("workers") or {}).get(wid) or {}, (f.get("workers") or {}).get(wid) or {}
    delta[wid + "_payout_hmc"] = float(fp.get("payout_hmc") or 0) - float(bp.get("payout_hmc") or 0)
    delta[wid + "_ranges"] = int(fp.get("accepted_ranges") or 0) - int(bp.get("accepted_ranges") or 0)
    delta[wid + "_attempts"] = int(fp.get("accepted_attempts") or 0) - int(bp.get("accepted_attempts") or 0)
if b.get("target_mod") and f.get("target_mod"):
    delta["target_mod"] = int(f["target_mod"]) - int(b["target_mod"])
if b.get("reward_per_m") is not None and f.get("reward_per_m") is not None:
    delta["reward_per_m"] = float(f["reward_per_m"]) - float(b["reward_per_m"])

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

if [[ -f "$DIFF_PY" ]]; then
  python3 "$DIFF_PY" "$OUT" "$JSONL" "$BASELINE" >>"$LOG" 2>&1 || true
  echo "[overnight] difficulty report: $OUT/DIFFICULTY.md"
fi

echo "[overnight] done summary=$SUMMARY log=$LOG"
