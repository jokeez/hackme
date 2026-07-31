#!/usr/bin/env bash
# Away-mode: wait for Day 7 → publish+TG → run Days 8–10 (24h each) → publish+TG.
#
# Intended under systemd --user while operator is offline.
# Does not restart a live Day 7 session; only chains after it.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
mkdir -p logs reports/oss-cve-watch-libheif
LOG="logs/away-libheif-autopilot-$(date -u +%Y%m%dT%H%M%SZ).log"
WATCH_DIR="$ROOT/reports/oss-cve-watch-libheif"
STATE="$WATCH_DIR/away-autopilot.json"
NODE_SSH="${NODE_SSH:-hackme-vps}"
DAY7_UNIT="${DAY7_UNIT:-hackme-libheif-day07-extra.service}"
POLL_SEC="${POLL_SEC:-60}"
# After Day 7 is published, fuzz+publish these days in order.
CHAIN_DAYS="${CHAIN_DAYS:-8 9 10}"
MAX_TIME="${MAX_TIME:-86400}"

log() { echo "[away-libheif $(date -Is)] $*" | tee -a "$LOG"; }

write_state() {
  python3 - <<PY
import json
from pathlib import Path
p = Path("$STATE")
data = {}
if p.is_file():
    try:
        data = json.loads(p.read_text())
    except Exception:
        data = {}
data.update({
    "updated_at": "$(date -Is)",
    "phase": "$1",
    "log": "$LOG",
    "chain_days": "$CHAIN_DAYS",
})
p.write_text(json.dumps(data, indent=2) + "\n")
PY
}

day_published() {
  local day="$1"
  python3 - "$day" <<'PY'
import json, sys
from pathlib import Path
day = int(sys.argv[1])
meta = Path("web/site/reports/oss-cve-watch-libheif/meta.json")
if not meta.is_file():
    raise SystemExit(1)
m = json.loads(meta.read_text())
ok = any(int(d.get("day") or 0) == day for d in (m.get("days") or []))
raise SystemExit(0 if ok else 1)
PY
}

best_out_for_day() {
  local day="$1"
  python3 - "$WATCH_DIR" "$day" <<'PY'
import json, sys
from pathlib import Path
watch, day = Path(sys.argv[1]), int(sys.argv[2])
cands = []
for d in watch.glob(f"day{day:02d}-libfuzzer-*"):
    s, r = d / "SESSION.json", d / "ROLLUP.json"
    if not (s.is_file() and r.is_file()):
        continue
    sess = json.loads(s.read_text())
    iters = int(sess.get("iterations") or 0)
    elapsed = float(sess.get("elapsed_sec") or 0)
    cands.append((elapsed, iters, str(d)))
if not cands:
    raise SystemExit(1)
cands.sort(reverse=True)
print(cands[0][2])
PY
}

gate_ok() {
  local out="$1"
  MIN_ITERATIONS="${MIN_ITERATIONS:-20000000}" MIN_ELAPSED_SEC="${MIN_ELAPSED_SEC:-82800}" \
    python3 - "$out" "${MIN_ITERATIONS}" "${MIN_ELAPSED_SEC}" <<'PY'
import json, sys
from pathlib import Path
out, min_it, min_el = Path(sys.argv[1]), int(sys.argv[2]), float(sys.argv[3])
s = json.loads((out / "SESSION.json").read_text()) if (out / "SESSION.json").is_file() else {}
r = json.loads((out / "ROLLUP.json").read_text())
iters = int(s.get("iterations") or (r.get("targets") or [{}])[0].get("iterations") or 0)
elapsed = float(s.get("elapsed_sec") or (r.get("targets") or [{}])[0].get("elapsed_sec") or 0)
corp = int(s.get("corpus_count") or 0)
cov = int(s.get("coverage_edges") or 0)
ok = iters >= min_it and elapsed >= min_el and corp > 0 and cov > 0
print(f"gate day-out iters={iters} elapsed={elapsed:.1f} corp={corp} cov={cov} ok={ok}")
raise SystemExit(0 if ok else 1)
PY
}

publish_day() {
  local day="$1"
  local out="$2"
  log "publish day=$day out=$out"
  DAY="$day" OUT="$out" NODE_SSH="$NODE_SSH" GIT_PUSH="${GIT_PUSH:-1}" POST_TELEGRAM=1 \
    bash "$ROOT/scripts/ops/publish_oss_cve_watch_libheif_day_finish.sh"
}

tg_refresh_day() {
  local day="$1"
  local news_id
  news_id="$(python3 - "$day" <<'PY'
import json, sys
from pathlib import Path
day = int(sys.argv[1])
needle = f"libheif-day{day:02d}"
items = json.loads(Path("web/site/assets/news.json").read_text()).get("items") or []
for it in items:
    if needle in it.get("id", ""):
        print(it["id"])
        break
PY
)"
  if [[ -z "$news_id" ]]; then
    return 1
  fi
  log "Telegram refresh FORCE_NEWS_ID=$news_id"
  FORCE_NEWS_ID="$news_id" NODE_SSH="$NODE_SSH" \
    bash "$ROOT/scripts/ops/publish_news_to_telegram.sh" || log "WARN: TG refresh failed for day $day"
}

wait_day7_finish() {
  # Writes gate-ready OUT path to $1 (avoids polluting command substitution with logs).
  local dest="${1:?}"
  write_state "wait_day7"
  log "waiting for $DAY7_UNIT to finish (poll ${POLL_SEC}s)"
  while systemctl --user is-active --quiet "$DAY7_UNIT" 2>/dev/null; do
    sleep "$POLL_SEC"
  done
  log "$DAY7_UNIT inactive — waiting for gate-ready OUT"
  local tries=0 out
  while true; do
    if out="$(best_out_for_day 7 2>/dev/null)"; then
      if gate_ok "$out"; then
        printf '%s\n' "$out" >"$dest"
        return 0
      fi
      log "day7 OUT present but below gate: $out"
    else
      log "no day7 OUT yet"
    fi
    tries=$((tries + 1))
    if [[ "$tries" -gt 180 ]]; then
      log "ERROR: day7 gate not reached after long wait"
      exit 2
    fi
    sleep "$POLL_SEC"
  done
}

ensure_day7_published() {
  local out7_file="$WATCH_DIR/.away_day7_out"
  if day_published 7; then
    log "Day 7 already in meta — skip wait/publish"
    return 0
  fi
  wait_day7_finish "$out7_file"
  local out7
  out7="$(cat "$out7_file")"
  if day_published 7; then
    log "Day 7 already published by day runner — TG-only refresh"
    tg_refresh_day 7 || publish_day 7 "$out7"
  else
    publish_day 7 "$out7"
  fi
  if ! day_published 7; then
    log "ERROR: Day 7 still not in meta after publish"
    exit 3
  fi
}

run_fuzz_day() {
  local day="$1"
  write_state "run_day${day}"
  log "starting Day $day (MAX_TIME=${MAX_TIME}s, publish+TG)"
  DAY="$day" TARGET=libheif MAX_TIME="$MAX_TIME" SKIP_REBUILD=1 \
  NODE_SSH="$NODE_SSH" GIT_PUSH="${GIT_PUSH:-1}" POST_TELEGRAM=1 \
    bash "$ROOT/scripts/ops/run_oss_cve_watch_libheif_libfuzzer_day.sh"
}

ensure_chain_day() {
  local day="$1"
  if day_published "$day"; then
    log "Day $day already published — skip"
    return 0
  fi
  run_fuzz_day "$day"
  if ! day_published "$day"; then
    local out
    out="$(best_out_for_day "$day")"
    publish_day "$day" "$out"
  fi
  if ! day_published "$day"; then
    log "ERROR: Day $day still not in meta after publish"
    exit 4
  fi
  write_state "day${day}_done"
}

main() {
  log "start ROOT=$ROOT chain_days=[$CHAIN_DAYS]"
  write_state "start"

  ensure_day7_published
  write_state "day7_done"

  # Avoid racing a leftover cadence unit
  systemctl --user stop hackme-libheif-24h.service 2>/dev/null || true
  systemctl --user disable hackme-libheif-24h.service 2>/dev/null || true

  local day
  for day in $CHAIN_DAYS; do
    ensure_chain_day "$day"
  done

  write_state "done"
  log "away autopilot complete (Day7 + chain [$CHAIN_DAYS] published)"
}

main "$@"
