#!/usr/bin/env bash
# Hub fuzz pool health fix: cleanup gates/stale, repair zombies, drain stale outbox, reclaim expired leases.
#
#   NODE_SSH=hackme-vps bash scripts/ops/pool_fuzz_health_fix.sh
#   DRY_RUN=1 NODE_SSH=hackme-vps bash scripts/ops/pool_fuzz_health_fix.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

DRY_RUN="${DRY_RUN:-0}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
REPAIR_ROUNDS="${REPAIR_ROUNDS:-5}"
REPAIR_LIMIT="${REPAIR_LIMIT:-200}"

log() { echo "[pool-fuzz-fix] $*"; }

snapshot() {
  if [[ -n "$NODE_SSH" ]]; then
    ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" 'python3 - <<"PY"
import json, sqlite3, time, urllib.request
now = int(time.time())
con = sqlite3.connect("file:/opt/hackme/data/coordinator_fuzz.db?mode=ro", uri=True)
c = con.cursor()
fs = json.load(urllib.request.urlopen("http://127.0.0.1:18081/api/fuzz/pool/stats", timeout=15))
stale = c.execute("SELECT COUNT(*) FROM fuzz_work_items WHERE status=\"leased\" AND lease_until<?", (now,)).fetchone()[0]
pend_ob = c.execute("SELECT COUNT(*) FROM fuzz_settle_outbox WHERE status=\"pending\"").fetchone()[0]
queued = c.execute("SELECT COUNT(*) FROM fuzz_work_items WHERE settle_run_status=\"queued\"").fetchone()[0]
paid = c.execute("SELECT COUNT(*) FROM fuzz_work_items WHERE settle_run_status=\"paid\"").fetchone()[0]
print(json.dumps({
  "work_done": fs.get("work_done"),
  "work_pending": fs.get("work_pending"),
  "campaigns_running": fs.get("campaigns_running"),
  "stale_leased": stale,
  "outbox_pending": pend_ob,
  "items_queued": queued,
  "items_paid": paid,
}))
PY'
  fi
}

log "before: $(snapshot || echo '{}')"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY_RUN — would run cleanup + drain + repair rounds"
  DRY_RUN=1 NODE_SSH="$NODE_SSH" bash "$ROOT/scripts/ops/drain_stale_fuzz_settle_outbox.sh"
  exit 0
fi

log "step 1: coordinator fuzz queue cleanup"
NODE_SSH="$NODE_SSH" COORD_URL="$COORD_URL" bash "$ROOT/scripts/ops/coordinator_fuzz_queue_cleanup.sh"

log "step 2: drain stale settle outbox + reclaim expired leases"
NODE_SSH="$NODE_SSH" bash "$ROOT/scripts/ops/drain_stale_fuzz_settle_outbox.sh"

# drain script sqlite3 over ssh sometimes returns 0 — force reclaim via hub python
if [[ -n "$NODE_SSH" ]]; then
  log "step 2b: force reclaim expired leases (hub python)"
  ssh -o BatchMode=yes -o ConnectTimeout=20 "$NODE_SSH" 'python3 - <<"PY"
import sqlite3, time
now = int(time.time())
con = sqlite3.connect("/opt/hackme/data/coordinator_fuzz.db", timeout=120)
c = con.cursor()
c.execute("PRAGMA busy_timeout=120000")
before = c.execute("SELECT COUNT(*) FROM fuzz_work_items WHERE status=\"leased\" AND lease_until<?", (now,)).fetchone()[0]
c.execute("""
UPDATE fuzz_work_items
SET status=\"pending\", lease_owner=\"\", lease_until=0, updated_at=?
WHERE status=\"leased\" AND lease_until < ?
""", (now, now))
released = c.rowcount
con.commit()
after = c.execute("SELECT COUNT(*) FROM fuzz_work_items WHERE status=\"leased\" AND lease_until<?", (now,)).fetchone()[0]
print(f"expired_before={before} released={released} expired_after={after}")
PY'
fi

log "step 3: repair-zombies rounds (limit=${REPAIR_LIMIT} x ${REPAIR_ROUNDS})"
COORD_ADMIN="${COORD_ADMIN_TOKEN:-}"
if [[ -z "$COORD_ADMIN" && -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]]; then
  COORD_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
fi
if [[ -z "$COORD_ADMIN" ]]; then
  log "WARN: no admin token — skipping repair-zombies API rounds"
else
  for i in $(seq 1 "$REPAIR_ROUNDS"); do
    out="$(ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" \
      "curl -fsS -X POST 'http://127.0.0.1:18081/api/fuzz/pool/campaigns/repair-zombies?limit=${REPAIR_LIMIT}' \
        -H 'X-Hackme-Admin-Token: ${COORD_ADMIN}'" 2>/dev/null || echo '{"ok":false}')"
    log "repair round $i: $out"
    repaired="$(printf '%s' "$out" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("repaired",0))' 2>/dev/null || echo 0)"
    [[ "${repaired:-0}" == "0" ]] && break
  done
fi

log "step 4: reconcile settlement state (clamp only)"
NODE_SSH="$NODE_SSH" bash "$ROOT/scripts/ops/reconcile_settlement_state.sh" || log "reconcile skipped"

log "after: $(snapshot || echo '{}')"
log "done"
