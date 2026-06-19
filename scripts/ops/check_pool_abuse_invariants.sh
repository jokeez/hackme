#!/usr/bin/env bash
# Pool abuse invariant checks for cron (exit 1 on anomaly).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
TOKEN_FILE="${TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_admin_token}"
MAX_FOUND_RATE="${MAX_FOUND_RATE:-0.35}"
MAX_WORKER_POOL_SHARE="${MAX_WORKER_POOL_SHARE:-0.55}"
MAX_BATCH_M="${MAX_BATCH_M:-32}"

TOKEN="${HACKME_COORDINATOR_ADMIN_TOKEN:-}"
[[ -n "$TOKEN" ]] || [[ ! -f "$TOKEN_FILE" ]] || TOKEN="$(tr -d '\r\n' <"$TOKEN_FILE")"
[[ -n "$TOKEN" ]] || { echo "[pool-abuse-check] missing admin token" >&2; exit 2; }

WS="$(curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $TOKEN" \
  "${COORD_URL%/}/api/work/stats?details=1")"

python3 - "$WS" "$MAX_FOUND_RATE" "$MAX_WORKER_POOL_SHARE" "$MAX_BATCH_M" <<'PY'
import json, sys
ws = json.loads(sys.argv[1])
max_found = float(sys.argv[2])
max_share = float(sys.argv[3])
max_batch_m = float(sys.argv[4])
submitted = float(ws.get("submitted_items") or 0)
found = float(ws.get("found_hits") or 0)
total_pay = float(ws.get("total_payout_hmc") or 0)
default_batch = float(ws.get("default_batch") or 0)
workers = ws.get("workers") or {}
issues = []
if submitted > 20 and found / submitted > max_found:
    issues.append(f"found_rate={found/submitted:.2%} > {max_found:.0%}")
if default_batch > 0 and default_batch / 1e6 > max_batch_m:
    issues.append(f"default_batch={default_batch/1e6:.1f}M > cap {max_batch_m}M")
for wid, row in workers.items():
    if not isinstance(row, dict):
        continue
    pay = float(row.get("payout_hmc") or 0)
    if total_pay > 0.01 and pay / total_pay > max_share:
        issues.append(f"worker {wid} pool_share={pay/total_pay:.1%}")
    peak = float(row.get("peak_hashrate_gh_s") or row.get("hashrate_gh_s") or 0)
    if peak > 256:
        issues.append(f"worker {wid} peak_gh={peak:.0f} > 256")
    att = float(row.get("accepted_attempts") or 0)
    if att > 0 and default_batch > 0 and att / max(submitted, 1) > default_batch * 4:
        issues.append(f"worker {wid} avg_attempts_per_submit suspicious")
if issues:
    print("[pool-abuse-check] ALERT")
    for i in issues:
        print(" ", i)
    sys.exit(1)
print(f"[pool-abuse-check] OK workers={len(workers)} total_payout_hmc={total_pay:.4f} submitted={submitted:.0f}")
PY
