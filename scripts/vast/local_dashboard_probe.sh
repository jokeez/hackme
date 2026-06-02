#!/usr/bin/env bash
# Probe local node APIs that feed dashboard #mining, #hardware, #chain (gitignored output).
# Run on operator PC while Vast workers are live and node is up on :8080.
#
#   bash scripts/vast/local_dashboard_probe.sh [worker_id]
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE="${BASE:-http://127.0.0.1:8080}"
BASE="${BASE%/}"
WID="${1:-}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-$ROOT/reports/vast-remote/local-ui-$STAMP}"
mkdir -p "$OUT"

ADMIN=""
[[ -f "$ROOT/.secrets/hackme_admin_token" ]] && ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token")"
hdr=()
[[ -n "$ADMIN" ]] && hdr=(-H "X-Hackme-Admin-Token: $ADMIN")

fetch() {
  local path="$1" out="$2"
  local code
  code="$(curl -sS -o "$out" -w '%{http_code}' --max-time 15 "${hdr[@]}" "${BASE}${path}" || echo 000)"
  echo "$path HTTP $code -> $out"
}

echo "=== local dashboard probe $STAMP base=$BASE worker=${WID:-any} ===" | tee "$OUT/summary.txt"

if ! curl -fsS --max-time 5 "${BASE}/api/status?lite=1" >/dev/null 2>&1; then
  echo "FAIL: node down at $BASE — start: bash scripts/ops/restart_linux_desktop_worker.sh" | tee -a "$OUT/summary.txt"
  exit 1
fi

fetch "/api/status?lite=1" "$OUT/status.json"
fetch "/api/work/stats?details=1" "$OUT/work_stats.json"
fetch "/api/network/stats" "$OUT/network_stats.json"
fetch "/api/hardware/tune" "$OUT/hardware_tune.json"

python3 - "$OUT" "$WID" <<'PY' | tee -a "$OUT/summary.txt"
import json, sys, pathlib
out, wid = sys.argv[1], sys.argv[2]
def load(name):
    p = pathlib.Path(out) / name
    if not p.exists():
        return {}
    try:
        return json.loads(p.read_text())
    except Exception:
        return {}
st = load("status.json")
work = load("work_stats.json")
net = load("network_stats.json")
hw = load("hardware_tune.json")
eco = st.get("economics") or {}
print("--- chain / difficulty (for #mining #chain UI) ---")
print("tip_height:", st.get("tip_height"), "canonical:", st.get("canonical_tip_height"))
print("pool_target_mod:", st.get("pool_target_mod"), "pool_gh:", st.get("pool_hashrate_gh_s"))
print("economics policy_hash:", (eco.get("policy_hash") or "")[:16], "...")
workers = work.get("workers") or work.get("worker_stats") or []
if isinstance(work.get("summary"), dict):
    workers = workers or work["summary"].get("workers") or []
print("work/stats worker rows:", len(workers) if isinstance(workers, list) else type(workers))
found = []
for w in (workers if isinstance(workers, list) else []):
    if not isinstance(w, dict):
        continue
    for k in ("worker_id", "id", "name"):
        v = str(w.get(k, ""))
        if wid and v == wid:
            found.append(w)
        elif not wid:
            found.append(w)
if wid:
    print("match worker", wid, ":", "YES" if found else "NO — check #mining table")
    for w in found[:3]:
        print(" ", {k: w.get(k) for k in ("worker_id", "id", "name", "hashrate_gh_s", "accepted_attempts", "last_seen") if k in w or w.get(k) is not None})
else:
    print("all workers (sample 8):")
    for w in (workers[:8] if isinstance(workers, list) else []):
        print(" ", w.get("worker_id") or w.get("id"), w.get("hashrate_gh_s"), w.get("hashrate"))
print("--- network (blocks / distribution hints) ---")
print("network keys:", list(net.keys())[:12] if isinstance(net, dict) else net)
print("--- hardware tune (#hardware tab) ---")
if hw:
    gpus = hw.get("gpus") or hw.get("devices") or []
    print("hardware tune gpus:", len(gpus) if isinstance(gpus, list) else hw.get("ok"))
else:
    print("hardware tune: empty or needs admin — open http://127.0.0.1:8080/#hardware")
print("--- UI manual ---")
print("Mining:  ", "http://127.0.0.1:8080/#mining")
print("Hardware:", "http://127.0.0.1:8080/#hardware")
print("Chain:   ", "http://127.0.0.1:8080/#chain")
PY

echo "[local-ui-probe] -> $OUT" | tee -a "$OUT/summary.txt"
