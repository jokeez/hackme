#!/usr/bin/env bash
# When HACKME_COORDINATOR_FUZZ_DB is separate, the main coordinator.db often keeps
# orphaned pre-split fuzz_* tables (GB of cold rows + freelist). Purge them + optional VACUUM.
#
# Safe for mining: hackme-node stays up. VACUUM briefly stops coordinator only.
#
#   NODE_SSH=hackme-vps bash scripts/ops/purge_coordinator_main_orphan_fuzz.sh
#   NODE_SSH=hackme-vps PURGE_VACUUM=1 bash scripts/ops/purge_coordinator_main_orphan_fuzz.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
DRY_RUN="${DRY_RUN:-0}"
PURGE_VACUUM="${PURGE_VACUUM:-0}"

if [[ -n "$NODE_SSH" ]]; then
  rsync -az "$ROOT/scripts/ops/purge_coordinator_main_orphan_fuzz.sh" "$NODE_SSH:$DEPLOY/scripts/ops/"
  ssh -o BatchMode=yes "$NODE_SSH" \
    "DEPLOY='$DEPLOY' DRY_RUN='$DRY_RUN' PURGE_VACUUM='$PURGE_VACUUM' bash '$DEPLOY/scripts/ops/purge_coordinator_main_orphan_fuzz.sh'"
  exit $?
fi

DEPLOY="${DEPLOY:-/opt/hackme}"
MAIN_DB="${HACKME_COORDINATOR_DB:-$DEPLOY/data/coordinator.db}"
FUZZ_DB="${HACKME_COORDINATOR_FUZZ_DB:-$DEPLOY/data/coordinator_fuzz.db}"
COORD_SERVICE="${COORD_SERVICE:-hackme-coordinator}"

log() { echo "[purge-main-orphan $(date -u +%H:%M:%S)] $*"; }

if [[ ! -f "$FUZZ_DB" ]]; then
  log "ERROR: fuzz DB missing ($FUZZ_DB) — refusing to wipe main fuzz tables"
  exit 1
fi
if [[ ! -f "$MAIN_DB" ]]; then
  log "ERROR: main DB missing $MAIN_DB"
  exit 1
fi

# Refuse if fuzz DB looks empty/uninitialized while main still has live-looking work.
fuzz_work="$(sqlite3 "$FUZZ_DB" "SELECT COUNT(*) FROM fuzz_work_items;" 2>/dev/null || echo 0)"
if [[ "${fuzz_work:-0}" -lt 100 ]]; then
  log "ERROR: fuzz DB work_items=$fuzz_work — abort (expected live separate fuzz ledger)"
  exit 1
fi

log "main=$MAIN_DB fuzz=$FUZZ_DB fuzz_work=$fuzz_work dry_run=$DRY_RUN vacuum=$PURGE_VACUUM"
sqlite3 "$MAIN_DB" "SELECT 'main_bytes', $(stat -c%s "$MAIN_DB"); PRAGMA freelist_count;" || true

TABLES=(
  fuzz_findings fuzz_corpus fuzz_work_items fuzz_native_queue
  fuzz_coverage_seen fuzz_pool_corpus fuzz_hunt_replay_queue
  fuzz_report_access_log fuzz_runtime_samples fuzz_corpus_namespace
  fuzz_settle_outbox fuzz_settle_applied fuzz_campaigns fuzz_campaign_escrow
)

if [[ "$DRY_RUN" == "1" ]]; then
  for t in "${TABLES[@]}"; do
    n="$(sqlite3 "$MAIN_DB" "SELECT COUNT(*) FROM $t;" 2>/dev/null || echo missing)"
    log "DRY $t=$n"
  done
  exit 0
fi

# Online deletes (coordinator may stay up). Live fuzz ledger is the separate fuzz DB.
MAIN_DB="$MAIN_DB" python3 - <<'PY'
import os, sqlite3
db=os.environ["MAIN_DB"]
tables=[
  "fuzz_findings","fuzz_corpus","fuzz_work_items","fuzz_native_queue",
  "fuzz_coverage_seen","fuzz_pool_corpus","fuzz_hunt_replay_queue",
  "fuzz_report_access_log","fuzz_runtime_samples","fuzz_corpus_namespace",
  "fuzz_settle_outbox","fuzz_settle_applied","fuzz_campaigns","fuzz_campaign_escrow",
]
con=sqlite3.connect(db, timeout=180)
con.execute("PRAGMA busy_timeout=180000")
c=con.cursor()
for t in tables:
    try:
        n=c.execute(f"SELECT COUNT(*) FROM {t}").fetchone()[0]
    except Exception as e:
        print(f"skip {t}: {e}", flush=True)
        continue
    if n==0:
        print(f"{t}: empty", flush=True)
        continue
    c.execute(f"DELETE FROM {t}")
    con.commit()
    print(f"{t}: deleted {n}", flush=True)
try:
    c.execute("PRAGMA wal_checkpoint(TRUNCATE)")
    print("wal_checkpoint: ok", flush=True)
except Exception as e:
    print(f"wal_checkpoint: {e}", flush=True)
print("freelist", c.execute("PRAGMA freelist_count").fetchone()[0], flush=True)
con.close()
PY

if [[ "$PURGE_VACUUM" == "1" ]]; then
  log "VACUUM INTO (stop $COORD_SERVICE briefly)"
  NEW="${MAIN_DB}.compact.$$"
  BACKUP="${MAIN_DB}.pre_vacuum.$(date -u +%Y%m%dT%H%M%SZ)"
  systemctl stop "$COORD_SERVICE"
  sleep 2
  if fuser "$MAIN_DB" >/dev/null 2>&1; then
    log "ERROR db busy after stop"
    fuser -v "$MAIN_DB" || true
    systemctl start "$COORD_SERVICE"
    exit 1
  fi
  sqlite3 "$MAIN_DB" "PRAGMA busy_timeout=600000; PRAGMA wal_checkpoint(TRUNCATE);"
  sqlite3 "$MAIN_DB" "VACUUM INTO '$NEW';"
  cp -a "$MAIN_DB" "$BACKUP"
  cp -a "${MAIN_DB}-wal" "${BACKUP}-wal" 2>/dev/null || true
  mv -f "$NEW" "$MAIN_DB"
  rm -f "${MAIN_DB}-wal" "${MAIN_DB}-shm"
  chown hackme:hackme "$MAIN_DB" 2>/dev/null || true
  chmod 644 "$MAIN_DB" 2>/dev/null || true
  systemctl start "$COORD_SERVICE"
  sleep 6
  systemctl is-active "$COORD_SERVICE"
  log "VACUUM done backup=$BACKUP size=$(stat -c%s "$MAIN_DB")"
fi

log "done main_bytes=$(stat -c%s "$MAIN_DB")"
