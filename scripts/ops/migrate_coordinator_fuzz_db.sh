#!/usr/bin/env bash
# One-shot: copy fuzz tables from coordinator.db → coordinator_fuzz.db on hub.
#
# Does NOT DROP fuzz tables from coordinator.db (archive until a later release).
#
# Usage (hub, root):
#   sudo bash scripts/ops/migrate_coordinator_fuzz_db.sh
#   MAIN_DB=/opt/hackme/data/coordinator.db FUZZ_DB=/opt/hackme/data/coordinator_fuzz.db \
#     sudo bash scripts/ops/migrate_coordinator_fuzz_db.sh
#
# Prefers bin/coordfuzzmigrate (OpenFuzz schema + column-safe copy). Falls back to
# sqlite ATTACH copy if the helper binary is missing.
#
# After success: HACKME_COORDINATOR_FUZZ_DB is written to .env.coord; start coordinator.
set -euo pipefail

DEPLOY="${DEPLOY:-/opt/hackme}"
MAIN_DB="${MAIN_DB:-${DEPLOY}/data/coordinator.db}"
FUZZ_DB="${FUZZ_DB:-${DEPLOY}/data/coordinator_fuzz.db}"
SERVICE="${COORDINATOR_SERVICE:-hackme-coordinator.service}"
ENV_COORD="${ENV_COORD:-${DEPLOY}/.env.coord}"
MIGRATE_BIN="${MIGRATE_BIN:-${DEPLOY}/bin/coordfuzzmigrate}"
REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
SKIP_STOP="${SKIP_STOP:-0}"
SKIP_START="${SKIP_START:-0}"

FUZZ_TABLES=(
  fuzz_campaigns
  fuzz_work_items
  fuzz_findings
  fuzz_corpus
  fuzz_pool_corpus
  fuzz_runtime_samples
  fuzz_coverage_seen
  fuzz_report_access_log
  fuzz_native_queue
  fuzz_campaign_escrow
  fuzz_settle_outbox
  fuzz_settle_applied
  fuzz_corpus_namespace
  hunt_harness_artifacts
)

ts() { date -u +"%Y-%m-%dT%H:%M:%SZ"; }
log() { echo "$(ts) [migrate-fuzz-db] $*"; }

[[ -f "$MAIN_DB" ]] || { log "ERROR: missing MAIN_DB=$MAIN_DB"; exit 1; }
command -v sqlite3 >/dev/null 2>&1 || { log "ERROR: sqlite3 missing"; exit 1; }

run_sql_main() {
  if id hackme >/dev/null 2>&1; then
    sudo -u hackme sqlite3 "$MAIN_DB" "PRAGMA busy_timeout=60000; $1" 2>/dev/null \
      || sqlite3 "$MAIN_DB" "PRAGMA busy_timeout=60000; $1"
  else
    sqlite3 "$MAIN_DB" "PRAGMA busy_timeout=60000; $1"
  fi
}

if [[ "$SKIP_STOP" != "1" ]]; then
  log "stop ${SERVICE}"
  systemctl stop "$SERVICE" || true
  sleep 2
  pkill -x coordinator 2>/dev/null || true
  sleep 1
fi

log "TRUNCATE checkpoint on main DB"
run_sql_main "PRAGMA wal_checkpoint(TRUNCATE);" || log "WARN: main TRUNCATE failed (continuing)"

if [[ -f "$FUZZ_DB" ]]; then
  log "ERROR: FUZZ_DB already exists: $FUZZ_DB (refuse overwrite)"
  log "Remove or move it, then re-run. Or set FUZZ_DB to a new path."
  if [[ "$SKIP_START" != "1" ]]; then
    systemctl start "$SERVICE" || true
  fi
  exit 1
fi

mkdir -p "$(dirname "$FUZZ_DB")"

copy_with_helper() {
  local bin="$1"
  log "copy via $bin"
  if id hackme >/dev/null 2>&1; then
    sudo -u hackme "$bin" -src "$MAIN_DB" -dst "$FUZZ_DB"
    chown hackme:hackme "$FUZZ_DB" "${FUZZ_DB}-wal" "${FUZZ_DB}-shm" 2>/dev/null || true
  else
    "$bin" -src "$MAIN_DB" -dst "$FUZZ_DB"
  fi
}

copy_with_sqlite() {
  log "copy via sqlite ATTACH (fallback)"
  local COPY_SQL
  COPY_SQL="$(mktemp)"
  {
    echo "PRAGMA busy_timeout=60000;"
    echo "ATTACH DATABASE '$(printf '%s' "$FUZZ_DB" | sed "s/'/''/g")' AS fuzz;"
    for t in "${FUZZ_TABLES[@]}"; do
      create_sql="$(sqlite3 "$MAIN_DB" "SELECT sql FROM sqlite_master WHERE type='table' AND name='$t';" 2>/dev/null || true)"
      if [[ -z "${create_sql:-}" ]]; then
        continue
      fi
      if [[ "$create_sql" == *"IF NOT EXISTS"* ]]; then
        echo "$create_sql" | sed "s/CREATE TABLE IF NOT EXISTS /CREATE TABLE IF NOT EXISTS fuzz./"
      else
        echo "$create_sql" | sed "s/CREATE TABLE /CREATE TABLE fuzz./"
      fi
      echo ";"
      while IFS= read -r idx_sql; do
        [[ -n "$idx_sql" ]] || continue
        echo "$idx_sql" | sed 's/ ON / ON fuzz./'
        echo ";"
      done < <(sqlite3 "$MAIN_DB" "SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name='$t' AND sql IS NOT NULL;")
      echo "INSERT INTO fuzz.$t SELECT * FROM main.$t;"
    done
    echo "DETACH DATABASE fuzz;"
  } >"$COPY_SQL"

  if id hackme >/dev/null 2>&1; then
    sudo -u hackme sqlite3 "$MAIN_DB" <"$COPY_SQL" 2>/dev/null || sqlite3 "$MAIN_DB" <"$COPY_SQL"
    chown hackme:hackme "$FUZZ_DB" "${FUZZ_DB}-wal" "${FUZZ_DB}-shm" 2>/dev/null || true
  else
    sqlite3 "$MAIN_DB" <"$COPY_SQL"
  fi
  rm -f "$COPY_SQL"

  # Sequence counters (best-effort)
  sqlite3 "$FUZZ_DB" "PRAGMA busy_timeout=60000;" >/dev/null 2>&1 || true
  while IFS='|' read -r name seq; do
    [[ -n "$name" ]] || continue
    sqlite3 "$FUZZ_DB" "INSERT INTO sqlite_sequence(name,seq) VALUES('$name',$seq)
      ON CONFLICT(name) DO UPDATE SET seq=excluded.seq;" 2>/dev/null || true
  done < <(sqlite3 "$MAIN_DB" "SELECT name, seq FROM sqlite_sequence WHERE name LIKE 'fuzz_%';" 2>/dev/null || true)
}

if [[ -x "$MIGRATE_BIN" ]]; then
  copy_with_helper "$MIGRATE_BIN"
elif [[ -d "$REPO_ROOT" ]] && command -v go >/dev/null 2>&1; then
  log "build coordfuzzmigrate from $REPO_ROOT"
  mkdir -p "$(dirname "$MIGRATE_BIN")"
  (cd "$REPO_ROOT" && go build -o "$MIGRATE_BIN" ./cmd/coordfuzzmigrate)
  copy_with_helper "$MIGRATE_BIN"
else
  copy_with_sqlite
fi

log "verify row counts"
FAIL=0
for t in "${FUZZ_TABLES[@]}"; do
  src="$(sqlite3 "$MAIN_DB" "SELECT COUNT(*) FROM $t;" 2>/dev/null || echo missing)"
  if [[ "$src" == "missing" ]]; then
    log "  $t: absent in main (ok)"
    continue
  fi
  dst="$(sqlite3 "$FUZZ_DB" "SELECT COUNT(*) FROM $t;" 2>/dev/null || echo ERR)"
  if [[ "$src" != "$dst" ]]; then
    log "ERROR: $t count main=$src fuzz=$dst"
    FAIL=1
  else
    log "  $t: $dst rows OK"
  fi
done
if [[ "$FAIL" -ne 0 ]]; then
  log "ERROR: count mismatch — leaving FUZZ_DB for inspection; not wiring env"
  exit 1
fi

if [[ -f "$ENV_COORD" ]]; then
  if grep -q '^HACKME_COORDINATOR_FUZZ_DB=' "$ENV_COORD" 2>/dev/null; then
    sed -i "s|^HACKME_COORDINATOR_FUZZ_DB=.*|HACKME_COORDINATOR_FUZZ_DB=${FUZZ_DB}|" "$ENV_COORD"
  else
    printf '\nHACKME_COORDINATOR_FUZZ_DB=%s\n' "$FUZZ_DB" >>"$ENV_COORD"
  fi
  log "set HACKME_COORDINATOR_FUZZ_DB in $ENV_COORD"
else
  umask 077
  printf 'HACKME_COORDINATOR_FUZZ_DB=%s\n' "$FUZZ_DB" >"$ENV_COORD"
  if id hackme >/dev/null 2>&1; then
    chown hackme:hackme "$ENV_COORD" 2>/dev/null || true
  fi
  log "created $ENV_COORD with HACKME_COORDINATOR_FUZZ_DB"
fi

sqlite3 "$FUZZ_DB" "PRAGMA busy_timeout=60000; PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null 2>&1 || true
if id hackme >/dev/null 2>&1; then
  chown hackme:hackme "$FUZZ_DB" "${FUZZ_DB}-wal" "${FUZZ_DB}-shm" 2>/dev/null || true
fi

log "NOTE: fuzz tables left in place on $MAIN_DB (archive; DROP later)"
if [[ "$SKIP_START" != "1" ]]; then
  log "start ${SERVICE}"
  systemctl start "$SERVICE"
  sleep 2
  if systemctl is-active --quiet "$SERVICE"; then
    log "coordinator active"
  else
    log "ERROR: coordinator failed to start"
    exit 1
  fi
fi
log "done: fuzz_db=$FUZZ_DB"
