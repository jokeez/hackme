#!/usr/bin/env bash
# One soak-test sample: pool difficulty, payouts, fuzz progress, hub WAL.
# Used by fleet_soak_10h.sh (every 10 min for ~10h).
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

COORD_URL="${COORD_URL:-http://132.243.112.100:18083}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
SOAK_DIR="${SOAK_DIR:-$ROOT/reports/fleet-soak-10h}"
INTERVAL_SEC="${INTERVAL_SEC:-600}"
DURATION_SEC="${DURATION_SEC:-36000}"

TOKEN="${COORD_ADMIN_TOKEN:-}"
if [[ -z "$TOKEN" && -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]]; then
  TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
fi
if [[ -z "$TOKEN" ]]; then
  echo "[soak] missing coordinator token" >&2
  exit 1
fi

mkdir -p "$SOAK_DIR/samples" "$SOAK_DIR/daily"

python3 - "$ROOT" "$SOAK_DIR" "$COORD_URL" "$TOKEN" "$NODE_SSH" <<'PY'
import json, os, subprocess, sys, time, urllib.error, urllib.request
from datetime import datetime, timezone
from pathlib import Path

root, soak_dir, coord_url, token, node_ssh = sys.argv[1:6]
soak = Path(soak_dir)
now = int(time.time())
ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
day = ts[:10]

def get_json(url, timeout=20):
    req = urllib.request.Request(url, headers={"X-Hackme-Admin-Token": token})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.load(r)

def ssh(cmd, timeout=25):
    try:
        out = subprocess.check_output(
            ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=12", node_ssh, cmd],
            text=True, timeout=timeout, stderr=subprocess.DEVNULL,
        )
        return out.strip()
    except Exception as e:
        return f"ERR:{e}"

fleet_names = {
    "ashwood", "blackout", "coldline", "digsite", "eastwind", "faraday", "graphite",
    "harbour", "ironclad", "jackknife", "keystone", "lantern", "mercury", "northstar", "overdrive",
}

stats = get_json(f"{coord_url}/api/work/stats?details=1")
fuzz = get_json(f"{coord_url}/api/fuzz/pool/stats")
workers = stats.get("workers") or {}

fleet_live = 0
fleet_gh = 0.0
fleet_payout = 0.0
per_worker = {}
for wid, w in workers.items():
    base = wid.replace("worker-", "")
    if base not in fleet_names:
        continue
    age = now - int(w.get("last_seen_unix") or 0)
    gh = float(w.get("hashrate_gh_s") or 0)
    pay = float(w.get("payout_hmc") or w.get("total_payout_hmc") or 0)
    if age < 120:
        fleet_live += 1
    fleet_gh += gh
    fleet_payout += pay
    per_worker[wid] = {"gh": gh, "age": age, "payout_hmc": pay}

# Fuzz bootstrap progress via hub SQL (prefer coordinator_fuzz.db after cutover)
fuzz_done = {}
sql = r'''
SELECT substr(c.title,35,10) AS lib,
  SUM(CASE WHEN w.status="done" THEN 1 ELSE 0 END) AS done
FROM fuzz_campaigns c
JOIN fuzz_work_items w ON w.campaign_id=c.id
WHERE c.status="running" AND c.title LIKE "%deep pool%" AND c.completed_at=0
GROUP BY c.id ORDER BY c.id;
'''
raw = ssh(
    f'python3 - <<\'EOS\'\nimport os,sqlite3\n'
    f'paths=["/opt/hackme/data/coordinator_fuzz.db","/opt/hackme/data/coordinator.db"]\n'
    f'path=next((p for p in paths if os.path.isfile(p)), paths[0])\n'
    f'con=sqlite3.connect(path, timeout=30)\n'
    f'con.execute("PRAGMA busy_timeout=30000")\n'
    f'for r in con.execute("""{sql}""").fetchall():\n'
    f'    print(r[0].strip(), r[1])\n'
    f'con.close()\nEOS'.replace("{sql}", sql)
)
if not raw.startswith("ERR:"):
    for line in raw.splitlines():
        parts = line.rsplit(" ", 1)
        if len(parts) == 2:
            fuzz_done[parts[0].strip()] = int(parts[1])

wal_bytes = 0
wal_line = ssh(
    "python3 - <<'EOS'\n"
    "import os\n"
    "total=0\n"
    "for p in ['/opt/hackme/data/coordinator.db-wal','/opt/hackme/data/coordinator_fuzz.db-wal']:\n"
    "  if os.path.isfile(p):\n"
    "    total += os.path.getsize(p)\n"
    "print(total)\n"
    "EOS"
)
try:
    wal_bytes = int(wal_line.split()[0])
except Exception:
    wal_bytes = 0

coord_restarts = ssh("journalctl -u hackme-coordinator --since '10 hours ago' --no-pager 2>/dev/null | grep -c 'Started hackme-coordinator' || echo 0")
try:
    coord_restarts = int(coord_restarts.splitlines()[0])
except Exception:
    coord_restarts = -1

row = {
    "ts": ts,
    "epoch": now,
    "pool_hashrate_gh_s": float(stats.get("pool_hashrate_gh_s") or 0),
    "target_mod": int(stats.get("target_mod") or 0),
    "target_mod_load_hint": int(stats.get("target_mod_load_hint") or 0),
    "target_mod_min": int(stats.get("target_mod_min") or 0),
    "target_mod_max": int(stats.get("target_mod_max") or 0),
    "reward_per_m": float(stats.get("reward_per_m") or 0),
    "total_payout_hmc": float(stats.get("total_payout_hmc") or 0),
    "fleet_live": fleet_live,
    "fleet_reported_gh_s": round(fleet_gh, 4),
    "fleet_payout_hmc": round(fleet_payout, 8),
    "workers_total": len(workers),
    "fuzz_work_done": int(fuzz.get("work_done") or 0),
    "fuzz_work_pending": int(fuzz.get("work_pending") or 0),
    "fuzz_campaigns_running": int(fuzz.get("campaigns_running") or 0),
    "fuzz_bootstrap_done": fuzz_done,
    "wal_bytes": wal_bytes,
    "coord_restarts_10h": coord_restarts,
    "per_worker": per_worker,
}

sample_path = soak / "samples" / f"{ts.replace(':', '')}.json"
sample_path.write_text(json.dumps(row, indent=2) + "\n")

tsv = soak / "metrics.tsv"
header = (
    "ts\tpool_gh\ttarget_mod\thint\treward_per_m\ttotal_payout_hmc\t"
    "fleet_live\tfleet_gh\tfleet_payout\tfuzz_done\twal_mb\tcoord_restarts\n"
)
if not tsv.is_file():
    tsv.write_text(header)
with tsv.open("a") as f:
    f.write(
        f"{ts}\t{row['pool_hashrate_gh_s']:.2f}\t{row['target_mod']}\t{row['target_mod_load_hint']}\t"
        f"{row['reward_per_m']:.10f}\t{row['total_payout_hmc']:.8f}\t"
        f"{row['fleet_live']}\t{row['fleet_reported_gh_s']:.2f}\t{row['fleet_payout_hmc']:.8f}\t"
        f"{row['fuzz_work_done']}\t{wal_bytes/1e6:.1f}\t{coord_restarts}\n"
    )

marker = soak / "STARTED.json"
if not marker.is_file():
    end_at = datetime.fromtimestamp(now + int(os.environ.get("DURATION_SEC", "36000")), timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    marker.write_text(json.dumps({
        "started_at": ts,
        "planned_end_at": end_at,
        "interval_sec": int(os.environ.get("INTERVAL_SEC", "600")),
        "coord_url": coord_url,
        "fleet_workers": sorted(fleet_names),
        "note": "15-worker PoH fleet GH 20-60 soak",
    }, indent=2) + "\n")

print(f"[soak] {ts} pool_gh={row['pool_hashrate_gh_s']:.1f} M={row['target_mod']} payout={row['total_payout_hmc']:.6f} fleet_live={fleet_live}/15 fuzz_done={row['fuzz_work_done']} wal={wal_bytes/1e6:.1f}MB")
PY
