#!/usr/bin/env bash
# Keep coordinator SQLite WAL small. Large WAL → nginx upstream timeouts on :18083 submit.
#
# Watches MAIN DB (PoH/work) and optional FUZZ DB with separate thresholds:
#   PoH  — tighter (submit path latency)
#   Fuzz — looser (high write rate; avoid bouncing coordinator every ~2h)
#
# Prefer ONLINE checkpoint (no downtime). Stop the coordinator when a DB WAL:
#   - >= HARD, OR
#   - stays >= STUCK after online PASSIVE+TRUNCATE failed to shrink (N consecutive)
#
# systemd timer: every ~10 minutes on hub.
set -euo pipefail

DEPLOY="${DEPLOY:-/opt/hackme}"
MAIN_DB="${COORDINATOR_DB:-${DEPLOY}/data/coordinator.db}"
FUZZ_DB="${COORDINATOR_FUZZ_DB:-${DEPLOY}/data/coordinator_fuzz.db}"

# PoH / main defaults (MiB → bytes)
MAIN_SOFT="${COORD_WAL_SOFT_THRESH:-67108864}"          # 64 MiB
MAIN_STUCK="${COORD_WAL_STUCK_THRESH:-134217728}"       # 128 MiB
MAIN_HARD="${COORD_WAL_HARD_THRESH:-268435456}"         # 256 MiB

# Fuzz defaults — higher ceiling so diggers don't force stop every ~2h
FUZZ_SOFT="${COORD_FUZZ_WAL_SOFT_THRESH:-100663296}"    # 96 MiB
FUZZ_STUCK="${COORD_FUZZ_WAL_STUCK_THRESH:-268435456}"  # 256 MiB
FUZZ_HARD="${COORD_FUZZ_WAL_HARD_THRESH:-402653184}"    # 384 MiB

STUCK_HITS_NEEDED="${COORD_WAL_STUCK_HITS:-3}"          # consecutive timer ticks before stop
SERVICE="${COORDINATOR_SERVICE:-hackme-coordinator.service}"
LOG="${LOG:-/opt/hackme/logs/coord-wal-guard.log}"
COORD_API="${COORD_WAL_HEALTH_URL:-http://127.0.0.1:18081/health}"
STUCK_STATE="${COORD_WAL_STUCK_STATE:-${DEPLOY}/logs/coord-wal-stuck.count}"

mkdir -p "$(dirname "$LOG")" "$(dirname "$STUCK_STATE")"
ts() { date -u +"%Y-%m-%dT%H:%M:%SZ"; }
log() { echo "$(ts) [coord-wal] $*" | tee -a "$LOG"; }

wal_size() {
  local db="$1"
  local wal="${db}-wal"
  if [[ -f "$wal" ]]; then
    stat -c '%s' "$wal" 2>/dev/null || echo 0
  else
    echo 0
  fi
}

# Returns soft|stuck|hard thresholds for a db path (space-separated).
thresh_for() {
  local db="$1"
  if [[ "$db" == "$FUZZ_DB" || "$(basename "$db")" == "coordinator_fuzz.db" ]]; then
    echo "$FUZZ_SOFT $FUZZ_STUCK $FUZZ_HARD"
  else
    echo "$MAIN_SOFT $MAIN_STUCK $MAIN_HARD"
  fi
}

run_sql() {
  local db="$1"
  local sql="$2"
  if id hackme >/dev/null 2>&1; then
    sudo -u hackme sqlite3 "$db" "PRAGMA busy_timeout=30000; ${sql}" 2>/dev/null \
      || sqlite3 "$db" "PRAGMA busy_timeout=30000; ${sql}"
  else
    sqlite3 "$db" "PRAGMA busy_timeout=30000; ${sql}"
  fi
}

reset_stuck() { echo 0 >"$STUCK_STATE"; }
bump_stuck() {
  local n=0
  [[ -f "$STUCK_STATE" ]] && n="$(tr -d '\r\n' <"$STUCK_STATE" 2>/dev/null || echo 0)"
  [[ "$n" =~ ^[0-9]+$ ]] || n=0
  n=$((n + 1))
  echo "$n" >"$STUCK_STATE"
  echo "$n"
}

# Build watch list
DB_PATHS=()
[[ -f "$MAIN_DB" ]] && DB_PATHS+=("$MAIN_DB")
if [[ -n "${COORDINATOR_FUZZ_DB:-}" || -f "$FUZZ_DB" ]]; then
  [[ -f "$FUZZ_DB" ]] && DB_PATHS+=("$FUZZ_DB")
fi

stop_truncate_restart() {
  local reason="$1"
  log "stop ${SERVICE} + TRUNCATE all watched DBs (${reason})"
  systemctl stop "$SERVICE" || true
  sleep 2
  pkill -x coordinator 2>/dev/null || true
  sleep 1

  local db before after
  for db in "${DB_PATHS[@]}"; do
    before="$(wal_size "$db")"
    if run_sql "$db" "PRAGMA wal_checkpoint(TRUNCATE);"; then
      after="$(wal_size "$db")"
      log "$(basename "$db") WAL ${before} -> ${after}"
    else
      log "ERROR: TRUNCATE failed for $db"
      systemctl start "$SERVICE" || true
      exit 1
    fi
  done

  reset_stuck
  systemctl start "$SERVICE"
  sleep 2
  if systemctl is-active --quiet "$SERVICE"; then
    log "coordinator active"
  else
    log "ERROR: coordinator failed to start"
    exit 1
  fi
  if curl -fsS --max-time 5 "$COORD_API" >/dev/null 2>&1; then
    log "health ok"
  else
    log "WARN: health probe failed after restart"
  fi
}

if [[ ${#DB_PATHS[@]} -eq 0 ]]; then
  log "no db under $MAIN_DB / $FUZZ_DB"
  exit 0
fi
command -v sqlite3 >/dev/null 2>&1 || { log "ERROR: sqlite3 missing"; exit 1; }

need_soft=0
need_stuck=0
need_hard=0
max_wal=0
worst_label=""

for db in "${DB_PATHS[@]}"; do
  w="$(wal_size "$db")"
  read -r soft stuck hard <<<"$(thresh_for "$db")"
  log "watch $(basename "$db") WAL=${w} soft=${soft} stuck=${stuck} hard=${hard}"
  [[ "${w:-0}" -gt "$max_wal" ]] && max_wal="$w" && worst_label="$(basename "$db")"
  [[ "${w:-0}" -ge "$soft" ]] && need_soft=1
  [[ "${w:-0}" -ge "$stuck" ]] && need_stuck=1
  [[ "${w:-0}" -ge "$hard" ]] && need_hard=1
done

if [[ "$need_soft" -eq 0 ]]; then
  reset_stuck
  exit 0
fi

# Online PASSIVE for each DB over its soft threshold
for db in "${DB_PATHS[@]}"; do
  w="$(wal_size "$db")"
  read -r soft stuck hard <<<"$(thresh_for "$db")"
  [[ "${w:-0}" -ge "$soft" ]] || continue
  log "$(basename "$db") WAL=${w} >= soft ${soft} — online PASSIVE"
  if run_sql "$db" "PRAGMA wal_checkpoint(PASSIVE);"; then
    :
  else
    log "WARN: PASSIVE failed for $(basename "$db") (busy?)"
  fi
  log "$(basename "$db") after PASSIVE WAL=$(wal_size "$db")"
done

need_soft=0
need_stuck=0
need_hard=0
max_wal=0
for db in "${DB_PATHS[@]}"; do
  w="$(wal_size "$db")"
  read -r soft stuck hard <<<"$(thresh_for "$db")"
  [[ "${w:-0}" -gt "$max_wal" ]] && max_wal="$w"
  [[ "${w:-0}" -ge "$soft" ]] && need_soft=1
  [[ "${w:-0}" -ge "$stuck" ]] && need_stuck=1
  [[ "${w:-0}" -ge "$hard" ]] && need_hard=1
done
if [[ "$need_soft" -eq 0 ]]; then
  reset_stuck
  exit 0
fi

# Online TRUNCATE attempt
for db in "${DB_PATHS[@]}"; do
  w="$(wal_size "$db")"
  read -r soft stuck hard <<<"$(thresh_for "$db")"
  [[ "${w:-0}" -ge "$soft" ]] || continue
  log "$(basename "$db") still ${w} — online TRUNCATE"
  run_sql "$db" "PRAGMA wal_checkpoint(TRUNCATE);" || log "WARN: online TRUNCATE failed for $(basename "$db")"
  log "$(basename "$db") after online TRUNCATE WAL=$(wal_size "$db")"
done

need_soft=0
need_stuck=0
need_hard=0
max_wal=0
worst_label=""
for db in "${DB_PATHS[@]}"; do
  w="$(wal_size "$db")"
  read -r soft stuck hard <<<"$(thresh_for "$db")"
  [[ "${w:-0}" -gt "$max_wal" ]] && max_wal="$w" && worst_label="$(basename "$db")"
  [[ "${w:-0}" -ge "$soft" ]] && need_soft=1
  [[ "${w:-0}" -ge "$stuck" ]] && need_stuck=1
  [[ "${w:-0}" -ge "$hard" ]] && need_hard=1
done

if [[ "$need_soft" -eq 0 ]]; then
  reset_stuck
  exit 0
fi

if [[ "$need_hard" -eq 1 ]]; then
  stop_truncate_restart "hard hit on ${worst_label} max_wal=${max_wal}"
  exit 0
fi

if [[ "$need_stuck" -eq 1 ]]; then
  stuck_n="$(bump_stuck)"
  log "WAL stuck max=${max_wal} (${worst_label}) consecutive=${stuck_n} need=${STUCK_HITS_NEEDED}"
  if [[ "${stuck_n}" -ge "$STUCK_HITS_NEEDED" ]]; then
    stop_truncate_restart "stuck x${stuck_n} on ${worst_label}"
  fi
  exit 0
fi

# Between soft and stuck: do NOT accumulate toward stop (was causing early bounce)
reset_stuck
log "max_wal=${max_wal} (${worst_label}) between soft and stuck — defer stop"
exit 0
