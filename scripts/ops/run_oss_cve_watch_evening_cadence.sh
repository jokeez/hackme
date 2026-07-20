#!/usr/bin/env bash
# Evening CVE Watch cadence (local time):
#   - Finish/publish day N around PUBLISH_HOUR (default 22:00)
#   - Immediately start day N+1 until next calendar day's UNTIL_HOUR (default 15:00)
#   - Repeat until END_DAY
#
# Must run under systemd --user (not agent sandbox) so the fuzzer is not killed.
#
#   START_DAY=11 END_DAY=14 bash scripts/ops/run_oss_cve_watch_evening_cadence.sh
#
# Env:
#   PUBLISH_HOUR=22 UNTIL_HOUR=15
#   SKIP_REBUILD=1 NODE_SSH=hackme-vps GIT_PUSH=1
#   WATCHDOG_SEC=45
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
mkdir -p logs reports/oss-cve-watch

START_DAY="${START_DAY:-11}"
END_DAY="${END_DAY:-14}"
PUBLISH_HOUR="${PUBLISH_HOUR:-22}"
UNTIL_HOUR="${UNTIL_HOUR:-15}"
SKIP_REBUILD="${SKIP_REBUILD:-1}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
GIT_PUSH="${GIT_PUSH:-1}"
WATCHDOG_SEC="${WATCHDOG_SEC:-45}"
STAMP_ROOT="${STAMP_ROOT:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG="logs/oss-cve-watch-evening-cadence-${STAMP_ROOT}.log"

log() { echo "[cadence $(date -Is)] $*"; }

secs_until_today_hour() {
  local hour="$1"
  python3 - "$hour" <<'PY'
import sys
from datetime import datetime, timedelta
h = int(sys.argv[1])
now = datetime.now()
t = now.replace(hour=h, minute=0, second=0, microsecond=0)
if t <= now:
    t += timedelta(days=1)
print(max(1, int((t - now).total_seconds())))
PY
}

secs_until_next_until_hour() {
  # From now to next UNTIL_HOUR crossing (if before UNTIL today → today; else tomorrow).
  python3 - "$UNTIL_HOUR" <<'PY'
import sys
from datetime import datetime, timedelta
h = int(sys.argv[1])
now = datetime.now()
t = now.replace(hour=h, minute=0, second=0, microsecond=0)
if t <= now:
    t += timedelta(days=1)
print(max(60, int((t - now).total_seconds())))
PY
}

fuzzer_pid() {
  pgrep -f '/\.cache/oss-libfuzzer-bin/nghttp2-asan-fuzzer' | head -1 || true
}

stop_fuzzer_graceful() {
  local pid
  pid="$(fuzzer_pid)"
  if [[ -z "$pid" ]]; then
    return 0
  fi
  log "SIGINT fuzzer pid=$pid (graceful stop for ROLLUP)"
  kill -INT "$pid" 2>/dev/null || true
  # libFuzzer flushes stats on INT; give it time then escalate
  local i
  for i in $(seq 1 90); do
    if ! kill -0 "$pid" 2>/dev/null; then
      log "fuzzer stopped after ${i}s"
      return 0
    fi
    sleep 1
  done
  log "WARN fuzzer still alive — SIGTERM"
  kill -TERM "$pid" 2>/dev/null || true
  sleep 5
  kill -KILL "$pid" 2>/dev/null || true
}

day_published() {
  local day="$1"
  local html="$ROOT/web/site/reports/oss-cve-watch/day$(printf '%02d' "$day").html"
  [[ -f "$html" ]] || return 1
  # WIP placeholders are not real publishes
  if grep -q 'NOT PUBLISHED YET' "$html" 2>/dev/null; then
    return 1
  fi
  if [[ -f "$ROOT/web/site/reports/oss-cve-watch/meta.json" ]]; then
    python3 - "$day" <<'PY'
import json,sys
day=int(sys.argv[1])
meta=json.load(open("web/site/reports/oss-cve-watch/meta.json"))
d=next((x for x in meta.get("days",[]) if int(x.get("day") or 0)==day), None)
iters=int((d or {}).get("iterations") or 0)
raise SystemExit(0 if iters>=50_000_000 else 1)
PY
    return $?
  fi
  return 0
}

latest_day_out() {
  local day="$1"
  find "$ROOT/reports/oss-cve-watch" -maxdepth 1 -type d -name "day$(printf '%02d' "$day")-libfuzzer-*" \
    | while read -r d; do
        [[ -f "$d/ROLLUP.json" ]] || continue
        echo "$(stat -c '%Y' "$d/ROLLUP.json") $d"
      done | sort -nr | head -1 | cut -d' ' -f2-
}

publish_day_if_needed() {
  local day="$1"
  if day_published "$day"; then
    # If published file exists but was a stub (<50M), allow re-publish from better OUT
    local existing_ok=1
    if [[ -f "$ROOT/web/site/reports/oss-cve-watch/meta.json" ]]; then
      existing_ok="$(python3 - "$day" <<'PY'
import json,sys
day=int(sys.argv[1])
meta=json.load(open("web/site/reports/oss-cve-watch/meta.json"))
d=next((x for x in meta.get("days",[]) if int(x.get("day") or 0)==day), None)
iters=int((d or {}).get("iterations") or 0)
print(1 if iters>=50_000_000 else 0)
PY
)"
    fi
    if [[ "$existing_ok" == "1" ]]; then
      log "day $day already published (depth OK)"
      return 0
    fi
    log "day $day HTML exists but depth too low — will re-publish from best ROLLUP"
    rm -f "$ROOT/web/site/reports/oss-cve-watch/day$(printf '%02d' "$day").html"
  fi
  local out
  out="$(latest_day_out "$day")"
  if [[ -z "$out" || ! -f "$out/ROLLUP.json" ]]; then
    log "ERROR: no ROLLUP for day $day — cannot publish"
    return 1
  fi
  # Prefer deepest session for this day (not merely newest stub)
  out="$(python3 - "$day" <<'PY'
import json,sys
from pathlib import Path
day=int(sys.argv[1])
best=None
best_it=-1
for d in Path("reports/oss-cve-watch").glob(f"day{day:02d}-libfuzzer-*"):
    sp=d/"SESSION.json"
    if not sp.is_file():
        continue
    s=json.loads(sp.read_text())
    it=int(s.get("iterations") or 0)
    if it>best_it:
        best_it=it
        best=str(d)
print(best or "")
PY
)"
  if [[ -z "$out" || ! -f "$out/ROLLUP.json" ]]; then
    log "ERROR: no usable ROLLUP for day $day"
    return 1
  fi
  log "publishing day $day from $out"
  DAY="$day" OUT="$out" NODE_SSH="$NODE_SSH" GIT_PUSH="$GIT_PUSH" \
    MIN_ITERATIONS="${MIN_ITERATIONS:-50000000}" MIN_ELAPSED_SEC="${MIN_ELAPSED_SEC:-3600}" \
    bash "$ROOT/scripts/ops/publish_oss_cve_watch_day_finish.sh" | tee -a "$LOG"
}

# Run one day with watchdog until deadline (unix epoch) or MAX_TIME wall budget.
# Writes ROLLUP via normal session scripts; does NOT publish (caller publishes).
run_day_until() {
  local day="$1"
  local deadline_epoch="$2"
  local stamp
  stamp="$(date -u +%Y%m%dT%H%M%SZ)-d$(printf '%02d' "$day")"
  local out="$ROOT/reports/oss-cve-watch/day$(printf '%02d' "$day")-libfuzzer-$stamp"
  mkdir -p "$out"

  log "=== DAY $day fuzz stamp=$stamp deadline=$(date -d "@$deadline_epoch" -Is) ==="

  while true; do
    local now_epoch remaining
    now_epoch="$(date +%s)"
    remaining=$((deadline_epoch - now_epoch))
    if [[ "$remaining" -le 30 ]]; then
      log "day $day deadline reached"
      stop_fuzzer_graceful
      break
    fi

    if [[ -f "$out/ROLLUP.json" ]]; then
      log "day $day ROLLUP ready"
      break
    fi

    local fp
    fp="$(fuzzer_pid)"
    if [[ -n "$fp" ]]; then
      # Another session may be running (e.g. leftover day11 unit) — wait / adopt
      sleep "$WATCHDOG_SEC"
      continue
    fi

    log "start/resume day $day remaining=${remaining}s"
    # Run in background so watchdog can INT at deadline
    (
      set +e
      DAY="$day" TARGET=nghttp2 MAX_TIME="$remaining" SKIP_REBUILD="$SKIP_REBUILD" \
        SKIP_PUBLISH=1 STAMP="$stamp" \
        bash "$ROOT/scripts/ops/run_oss_cve_watch_libfuzzer_day.sh" >>"$LOG" 2>&1
      echo $? >"$out/fuzz.rc"
    ) &
    local wrap_pid=$!

    while kill -0 "$wrap_pid" 2>/dev/null; do
      now_epoch="$(date +%s)"
      if [[ "$now_epoch" -ge "$deadline_epoch" ]]; then
        log "deadline hit during day $day — graceful stop"
        stop_fuzzer_graceful
        wait "$wrap_pid" 2>/dev/null || true
        break 2
      fi
      # If fuzzer binary died but wrapper still spinning, break to restart
      sleep "$WATCHDOG_SEC"
      if ! kill -0 "$wrap_pid" 2>/dev/null; then
        break
      fi
      fp="$(fuzzer_pid)"
      if [[ -z "$fp" ]] && [[ ! -f "$out/ROLLUP.json" ]]; then
        # wrapper alive but fuzzer gone — wait a bit for export
        sleep 15
        if [[ ! -f "$out/ROLLUP.json" ]] && [[ -z "$(fuzzer_pid)" ]]; then
          log "WARN fuzzer vanished without ROLLUP — will restart"
          kill "$wrap_pid" 2>/dev/null || true
          wait "$wrap_pid" 2>/dev/null || true
          break
        fi
      fi
    done
    wait "$wrap_pid" 2>/dev/null || true

    if [[ -f "$out/ROLLUP.json" ]]; then
      break
    fi
    # New stamp only if previous session never produced rollup path; keep same OUT for copy
    # If session wrote to sessions/$stamp but day OUT missing rollup, try copy
    local sess="$ROOT/reports/oss-cve-libfuzzer/nghttp2/sessions/$stamp"
    if [[ -f "$sess/ROLLUP.json" ]]; then
      cp "$sess/ROLLUP.json" "$out/ROLLUP.json"
      cp "$sess/SESSION.json" "$out/SESSION.json" 2>/dev/null || true
      break
    fi
    # Fresh stamp for next attempt (corpus persists)
    stamp="$(date -u +%Y%m%dT%H%M%SZ)-d$(printf '%02d' "$day")-r$(date +%s)"
    out="$ROOT/reports/oss-cve-watch/day$(printf '%02d' "$day")-libfuzzer-$stamp"
    mkdir -p "$out"
    log "retry day $day with new stamp=$stamp"
  done

  if [[ ! -f "$out/ROLLUP.json" ]]; then
    # Last-resort: export from latest session matching day stamp prefix
    local sess
    sess="$(ls -td "$ROOT/reports/oss-cve-libfuzzer/nghttp2/sessions/"*"-d$(printf '%02d' "$day")"* 2>/dev/null | head -1 || true)"
    if [[ -n "$sess" && -f "$sess/fuzzer.log" && ! -f "$sess/ROLLUP.json" ]]; then
      local bin
      bin="$ROOT/.cache/oss-libfuzzer-bin/nghttp2-asan-fuzzer"
      python3 "$ROOT/scripts/ops/export_oss_libfuzzer_session.py" nghttp2 "$sess" "$bin" >>"$LOG" 2>&1 || true
    fi
    if [[ -n "${sess:-}" && -f "$sess/ROLLUP.json" ]]; then
      out="$ROOT/reports/oss-cve-watch/day$(printf '%02d' "$day")-libfuzzer-$(basename "$sess")"
      mkdir -p "$out"
      cp "$sess/ROLLUP.json" "$out/ROLLUP.json"
      cp "$sess/SESSION.json" "$out/SESSION.json" 2>/dev/null || true
    fi
  fi

  [[ -f "$out/ROLLUP.json" ]] || {
    log "FATAL: day $day finished without ROLLUP"
    return 1
  }
  # Expose path for publish
  echo "$out" >"$ROOT/reports/oss-cve-watch/.last_out_day$(printf '%02d' "$day")"
  log "day $day fuzz complete out=$out verdict=$(python3 -c "import json;print(json.load(open('$out/ROLLUP.json'))['verdict'])")"
}

wait_until_hour() {
  local hour="$1"
  local sec
  sec="$(secs_until_today_hour "$hour")"
  # If we're within 2 min after hour already today, secs_until jumps to tomorrow — OK for next cycle
  log "sleep ${sec}s until local hour=$hour:00"
  sleep "$sec"
}

# --- main ---
{
  log "start START_DAY=$START_DAY END_DAY=$END_DAY publish=${PUBLISH_HOUR}:00 until=${UNTIL_HOUR}:00"
  log "log=$LOG"

  day="$START_DAY"

  # Phase 1: if starting mid-day before publish hour, fuzz until publish hour then publish.
  now_h="$(date +%H)"
  if [[ "$day" -le "$END_DAY" ]]; then
    if day_published "$day"; then
      log "day $day already published — skip fuzz"
    elif [[ "$now_h" -lt "$PUBLISH_HOUR" ]]; then
      deadline="$(date -d "today ${PUBLISH_HOUR}:00" +%s)"
      # Adopt existing transient day unit if present: INT at deadline, let it finalize
      if [[ -n "$(fuzzer_pid)" ]]; then
        log "adopting live fuzzer until $(date -d "@$deadline" -Is)"
        while [[ "$(date +%s)" -lt "$deadline" ]]; do
          if [[ -z "$(fuzzer_pid)" ]]; then
              log "live fuzzer died early — stop old unit and restart with remaining budget"
              systemctl --user stop hackme-cve-day11.service 2>/dev/null || true
              # wait briefly for unit teardown
              sleep 3
              run_day_until "$day" "$deadline" || true
              break
            fi
          sleep "$WATCHDOG_SEC"
        done
        stop_fuzzer_graceful
        log "waiting up to 15m for day $day ROLLUP/publish"
        for _ in $(seq 1 180); do
          if day_published "$day"; then break; fi
          if [[ -n "$(latest_day_out "$day")" ]]; then break; fi
          sleep 5
        done
        if ! day_published "$day"; then
          if [[ -z "$(latest_day_out "$day")" ]]; then
            sess="$(readlink -f "$ROOT/reports/oss-cve-libfuzzer/nghttp2/LATEST_SESSION" 2>/dev/null || true)"
            if [[ -n "$sess" && -f "$sess/fuzzer.log" ]]; then
              bin="$ROOT/.cache/oss-libfuzzer-bin/nghttp2-asan-fuzzer"
              if [[ ! -f "$sess/ROLLUP.json" ]]; then
                python3 "$ROOT/scripts/ops/export_oss_libfuzzer_session.py" nghttp2 "$sess" "$bin" >>"$LOG" 2>&1 || true
              fi
              out="$ROOT/reports/oss-cve-watch/day$(printf '%02d' "$day")-libfuzzer-$(basename "$sess")"
              mkdir -p "$out"
              cp -f "$sess/ROLLUP.json" "$out/ROLLUP.json" 2>/dev/null || true
              cp -f "$sess/SESSION.json" "$out/SESSION.json" 2>/dev/null || true
            fi
          fi
          publish_day_if_needed "$day" || log "WARN publish day $day failed"
        fi
      else
        run_day_until "$day" "$deadline"
        publish_day_if_needed "$day" || exit 3
      fi
    else
      # After publish hour: short finalize then publish
      deadline=$(( $(date +%s) + 120 ))
      run_day_until "$day" "$deadline"
      publish_day_if_needed "$day" || exit 3
    fi
    day=$((day + 1))
  fi

  # Phase 2: remaining days — start at publish hour (or immediately if already there),
  # fuzz until next UNTIL_HOUR, publish, then wait until next publish hour.
  while [[ "$day" -le "$END_DAY" ]]; do
    now_h="$(date +%H)"
    if [[ "$now_h" -lt "$PUBLISH_HOUR" ]]; then
      wait_until_hour "$PUBLISH_HOUR"
    fi

    until_secs="$(secs_until_next_until_hour)"
    deadline=$(( $(date +%s) + until_secs ))
    log "schedule day $day → until $(date -d "@$deadline" -Is) (${until_secs}s)"
    run_day_until "$day" "$deadline"
    publish_day_if_needed "$day" || log "WARN publish day $day failed — continuing"
    day=$((day + 1))

    if [[ "$day" -le "$END_DAY" ]]; then
      now_h="$(date +%H)"
      if [[ "$now_h" -lt "$PUBLISH_HOUR" ]]; then
        wait_until_hour "$PUBLISH_HOUR"
      fi
    fi
  done

  log "ALL DONE through day $END_DAY"
} 2>&1 | tee -a "$LOG"
