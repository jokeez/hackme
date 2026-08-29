#!/usr/bin/env bash
# Revive stuck bootstrap deep-pool campaigns on hub coordinator:
# status=running but all work cancelled + completed_at set → no_fuzz_work forever.
#
# Run ON hub (or via ssh):
#   bash scripts/ops/revive_bootstrap_deep_pool_campaigns.sh
#
# Env:
#   DB=/opt/hackme/data/coordinator_fuzz.db
#   BUDGET_SECONDS=604800   # keep Tick from re-completing by wall clock
set -euo pipefail

DB="${DB:-/opt/hackme/data/coordinator_fuzz.db}"
BUDGET_SECONDS="${BUDGET_SECONDS:-604800}"
NOW="$(date +%s)"

# Do NOT revive legacy 24k-run deep-pool walls (cancel those instead).
# Keep this list empty unless intentionally re-arming small-budget campaigns.
IDS=(
)

if [[ ${#IDS[@]} -eq 0 ]]; then
  echo "[revive] IDS empty — nothing to revive (legacy 24k walls stay cancelled)"
  exit 0
fi

if [[ ! -f "$DB" ]]; then
  echo "[revive] missing DB $DB" >&2
  exit 1
fi

id_list="$(printf "'%s'," "${IDS[@]}")"
id_list="${id_list%,}"

echo "[revive] DB=$DB now=$NOW budget_seconds=$BUDGET_SECONDS"
echo "[revive] campaigns: ${IDS[*]}"

# Coordinator holds the DB — need busy_timeout / retries.
python3 - "$DB" "$NOW" "$BUDGET_SECONDS" "${IDS[@]}" <<'PY'
import sqlite3, sys, time
db, now, budget = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
ids = sys.argv[4:]
last = None
for attempt in range(1, 16):
    try:
        con = sqlite3.connect(db, timeout=60)
        con.execute("PRAGMA busy_timeout=60000")
        con.execute("BEGIN IMMEDIATE")
        ph = ",".join("?" * len(ids))
        con.execute(
            f"UPDATE fuzz_campaigns SET status='running', completed_at=0, started_at=?, budget_seconds=? WHERE id IN ({ph})",
            [now, budget, *ids],
        )
        cur = con.execute(
            f"UPDATE fuzz_work_items SET status='pending', lease_owner='', lease_until=0, attempts=0, last_error='', updated_at=? WHERE campaign_id IN ({ph}) AND status='cancelled'",
            [now, *ids],
        )
        n = cur.rowcount
        con.commit()
        print(f"[revive] revived_rows={n} attempt={attempt}")
        con.close()
        break
    except Exception as e:
        last = e
        print(f"[revive] attempt {attempt} fail: {e}", flush=True)
        time.sleep(2)
else:
    raise SystemExit(f"[revive] failed: {last}")
PY

echo "[revive] post-state:"
sqlite3 -header -column "$DB" <<SQL
SELECT c.id,
       c.status,
       c.completed_at,
       c.budget_seconds,
       SUM(CASE WHEN w.status='pending' THEN 1 ELSE 0 END) AS pending,
       SUM(CASE WHEN w.status='cancelled' THEN 1 ELSE 0 END) AS cancelled,
       SUM(CASE WHEN w.status='done' THEN 1 ELSE 0 END) AS done
  FROM fuzz_campaigns c
  LEFT JOIN fuzz_work_items w ON w.campaign_id=c.id
 WHERE c.id IN (${id_list})
 GROUP BY c.id
 ORDER BY c.id;
SQL

echo "[revive] ok — workerfuzz should claim again (avoid /api/fuzz/pool/campaigns/cleanup-stale for these)"
