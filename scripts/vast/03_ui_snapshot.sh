#!/usr/bin/env bash
# Snapshot coordinator work/stats + pool/stats (compare with hackme.tech dashboard UI).
set -euo pipefail
PACK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-$PACK_ROOT/env.vast}"
REPORT="${REPORT:-$PACK_ROOT/reports/vast-session}"
mkdir -p "$REPORT"

if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$ENV_FILE"
  set +a
fi

COORD="${COORD_URL:-https://hackme.tech/pool/coordinator}"
COORD="${COORD%/}"
WID="${WORKER_ID:-}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="$REPORT/ui-snapshot-$STAMP.json"

tok="${COORD_TOKEN:-}"
hdr=()
[[ -n "$tok" ]] && hdr=(-H "X-Hackme-Admin-Token: $tok")

work="$(curl -fsS --max-time 20 "${hdr[@]}" "${COORD}/api/work/stats?details=1" 2>/dev/null || echo '{}')"
pool="$(curl -fsS --max-time 20 "${COORD}/api/pool/stats" 2>/dev/null || echo '{}')"

python3 - "$OUT" "$WID" "$STAMP" "$work" "$pool" <<'PY'
import json, sys
out, wid, stamp, work_s, pool_s = sys.argv[1:6]
try:
    work = json.loads(work_s)
except Exception:
    work = {"_raw": work_s[:2000]}
try:
    pool = json.loads(pool_s)
except Exception:
    pool = {"_raw": pool_s[:2000]}
workers = []
for key in ("workers", "worker_stats", "rigs"):
    if isinstance(work.get(key), list):
        workers = work[key]
        break
if not workers and isinstance(work.get("summary"), dict):
    workers = work["summary"].get("workers") or []
match = None
for w in workers:
    if not isinstance(w, dict):
        continue
    for k in ("worker_id", "id", "name"):
        if str(w.get(k, "")) == wid:
            match = w
            break
    if match:
        break
doc = {
    "stamp": stamp,
    "worker_id": wid,
    "worker_found_in_stats": match is not None,
    "worker_row": match,
    "pool_hashrate_gh_s": pool.get("pool_hashrate_gh_s") or pool.get("hashrate_gh_s"),
    "pool_workers": pool.get("workers") or pool.get("online_workers"),
    "work_summary": work.get("summary"),
}
with open(out, "w") as f:
    json.dump(doc, f, indent=2)
print(json.dumps(doc, indent=2))
PY

echo "[ui-snapshot] -> $OUT"
echo "[ui-snapshot] Open https://hackme.tech/dashboard.html#mining and verify worker_id=$WID appears with GH/s"
