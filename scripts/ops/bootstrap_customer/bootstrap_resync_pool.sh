#!/usr/bin/env bash
# Push bootstrap node pool campaigns to production coordinator.
set -euo pipefail
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
COORD="${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}"
COORD="${COORD%/}"
COORD_ADMIN="$(tr -d '\r\n' <"${COORD_ADMIN_FILE:-$INSTALL/.secrets/coordinator_admin.token}")"
DB="${HACKME_DATA_DIR:-$INSTALL/data}/hackme.db"
CID_FILTER="${CAMPAIGN_ID:-}"
[[ -n "$COORD_ADMIN" ]] || { echo "[bootstrap-resync] missing coordinator admin token" >&2; exit 1; }
[[ -f "$DB" ]] || { echo "[bootstrap-resync] missing db $DB" >&2; exit 1; }

python3 - "$DB" "$CID_FILTER" <<'PY' | while read -r body; do
import json, sqlite3, sys
db, filt = sys.argv[1], sys.argv[2].strip()
con = sqlite3.connect(db)
cur = con.execute(
    """SELECT id, campaign_type, title, description, status, budget_runs, budget_seconds, config_json, COALESCE(owner_ref,'')
       FROM fuzz_campaigns
       WHERE json_extract(config_json, '$.pool_distributed') IN (1, 'true', '1')
         AND status IN ('planned', 'running')
       ORDER BY created_at DESC"""
)
for row in cur:
    cid = row[0]
    if filt and cid != filt:
        continue
    cfg = json.loads(row[7] or "{}")
    cfg["pool_distributed"] = True
    owner = (row[8] or "").strip()
    body = {
        "id": row[0],
        "campaign_type": row[1] or "property",
        "title": row[2] or row[0],
        "description": row[3] or "",
        "owner_ref": owner,
        "status": "running",
        "budget_runs": row[5],
        "budget_seconds": row[6],
        "config": cfg,
    }
    print(json.dumps(body, separators=(",", ":")))
con.close()
PY
  cid="$(jq -r '.id' <<<"$body")"
  echo "[bootstrap-resync] POST $cid"
  curl -fsS -X POST "$COORD/api/fuzz/pool/campaigns" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $COORD_ADMIN" \
    -d "$body" | jq -c '{ok,campaign_id,pool_distributed,work_queue}' || true
done
