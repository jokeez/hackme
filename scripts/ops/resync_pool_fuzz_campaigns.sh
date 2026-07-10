#!/usr/bin/env bash
# Push local pool-distributed fuzz campaigns to the coordinator (repair failed pool_sync).
#
#   bash scripts/ops/resync_pool_fuzz_campaigns.sh
#   CAMPAIGN_ID=campaign-audit-... bash scripts/ops/resync_pool_fuzz_campaigns.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd curl jq python3 sqlite3

COORD="${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}"
COORD="${COORD%/}"
COORD_ADMIN="$(tr -d '\r\n' <"${COORD_ADMIN_FILE:-$ROOT/.secrets/hackme_coordinator_admin_token}" 2>/dev/null || true)"
[[ -n "$COORD_ADMIN" ]] || fail "missing .secrets/hackme_coordinator_admin_token"

DB="${HACKME_DATA_DIR:-$ROOT/data}/hackme.db"
[[ -f "$DB" ]] || DB="$ROOT/logs/desktop/data/hackme.db"
[[ -f "$DB" ]] || fail "hackme.db not found"

CID_FILTER="${CAMPAIGN_ID:-}"
log() { echo "[resync-pool-fuzz] $*"; }

mapfile -t ROWS < <(python3 - "$DB" "$CID_FILTER" <<'PY'
import json, sqlite3, sys
db, filt = sys.argv[1], sys.argv[2].strip()
con = sqlite3.connect(db)
cur = con.execute(
    """SELECT id, campaign_type, title, description, status, budget_runs, budget_seconds, config_json
       FROM fuzz_campaigns
       WHERE json_extract(config_json, '$.pool_distributed') IN (1, 'true', '1')
         AND status IN ('planned', 'running')
       ORDER BY created_at DESC"""
)
for row in cur:
    cid = row[0]
    if filt and cid != filt:
        continue
    title = (row[2] or "").lower()
    if "gate-audit" in title or title == "pool sync node gate":
        continue
    if cid.startswith("pool-sync-node-"):
        continue
    cfg = json.loads(row[7] or "{}")
    cfg["pool_distributed"] = True
    body = {
        "id": row[0],
        "campaign_type": row[1] or "property",
        "title": row[2] or row[0],
        "description": row[3] or "",
        "status": "running",
        "budget_runs": row[5],
        "budget_seconds": row[6],
        "config": cfg,
    }
    print(json.dumps(body, separators=(",", ":")))
con.close()
PY
)

if [[ ${#ROWS[@]} -eq 0 ]]; then
  log "no pool campaigns to sync"
  exit 0
fi

ok=0
fail=0
for body in "${ROWS[@]}"; do
  cid="$(echo "$body" | jq -r '.id')"
  log "POST $cid"
  if curl -fsS --max-time 90 -X POST "${COORD}/api/fuzz/pool/campaigns" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $COORD_ADMIN" \
    -d "$body" | jq -e '.ok == true' >/dev/null; then
    log "OK $cid"
    ok=$((ok + 1))
  else
    log "FAIL $cid"
    fail=$((fail + 1))
  fi
done
log "done ok=$ok fail=$fail"
[[ "$fail" -eq 0 ]]
