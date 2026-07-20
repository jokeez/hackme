#!/usr/bin/env bash
# libheif OSS CVE Watch — fixed 24h day windows, 24/7 fuzz, auto chain Day N → N+1.
#
# Each calendar day = exactly DAY_SEC (default 86400) from series anchor.
# Anchor = first session start (or ANCHOR_EPOCH env). No random wall times.
#
#   bash scripts/ops/run_oss_cve_watch_libheif_24h_cadence.sh
#   START_DAY=1 END_DAY=14 ANCHOR_EPOCH=1721473963 bash scripts/ops/...
#
# Run under systemd --user on VPS (survives SSH disconnect).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
mkdir -p logs reports/oss-cve-watch-libheif

TARGET="${TARGET:-libheif}"
START_DAY="${START_DAY:-1}"
END_DAY="${END_DAY:-14}"
DAY_SEC="${DAY_SEC:-86400}"
WATCHDOG_SEC="${WATCHDOG_SEC:-45}"
SKIP_REBUILD="${SKIP_REBUILD:-1}"
SKIP_PUBLISH="${SKIP_PUBLISH:-0}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
GIT_PUSH="${GIT_PUSH:-1}"
WATCH_DIR="$ROOT/reports/oss-cve-watch-libheif"
STATE="$WATCH_DIR/cadence.json"
STAMP_ROOT="${STAMP_ROOT:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG="logs/oss-cve-watch-libheif-24h-${STAMP_ROOT}.log"

log() { echo "[libheif-24h $(date -Is)] $*" | tee -a "$LOG"; }

fuzzer_pid() {
  pgrep -f "file_fuzzer.*oss-cve-libfuzzer/${TARGET}/corpus" | head -1 || true
}

stop_fuzzer_graceful() {
  local pid
  pid="$(fuzzer_pid)"
  [[ -n "$pid" ]] || return 0
  log "SIGINT fuzzer pid=$pid"
  kill -INT "$pid" 2>/dev/null || true
  local i
  for i in $(seq 1 120); do
    kill -0 "$pid" 2>/dev/null || { log "fuzzer stopped after ${i}s"; return 0; }
    sleep 1
  done
  kill -TERM "$pid" 2>/dev/null || true
  sleep 5
  kill -KILL "$pid" 2>/dev/null || true
}

session_stamp_to_epoch() {
  local stamp="$1"
  python3 - "$stamp" <<'PY'
import sys
from datetime import datetime, timezone
s = sys.argv[1].split("-d")[0].split("-r")[0]
if len(s) < 15 or s[8] != "T":
    raise SystemExit(1)
dt = datetime.strptime(s[:15], "%Y%m%dT%H%M%S").replace(tzinfo=timezone.utc)
print(int(dt.timestamp()))
PY
}

load_or_init_state() {
  mkdir -p "$WATCH_DIR"
  if [[ -n "${ANCHOR_EPOCH:-}" ]]; then
    python3 - <<PY
import json
from pathlib import Path
p = Path("$STATE")
data = {
  "anchor_epoch": int("$ANCHOR_EPOCH"),
  "start_day": int("$START_DAY"),
  "end_day": int("$END_DAY"),
  "day_sec": int("$DAY_SEC"),
  "target": "$TARGET",
}
p.write_text(json.dumps(data, indent=2) + "\\n")
print(data["anchor_epoch"])
PY
    return 0
  fi
  if [[ -f "$STATE" ]]; then
    return 0
  fi
  # Adopt earliest libheif session dir as Day 1 anchor
  local first_sess epoch
  first_sess="$(ls -1 "$ROOT/reports/oss-cve-libfuzzer/$TARGET/sessions/" 2>/dev/null | sort | head -1 || true)"
  if [[ -n "$first_sess" ]]; then
    epoch="$(session_stamp_to_epoch "$first_sess" 2>/dev/null || true)"
  fi
  epoch="${epoch:-$(date +%s)}"
  ANCHOR_EPOCH="$epoch" START_DAY="$START_DAY" END_DAY="$END_DAY" DAY_SEC="$DAY_SEC" TARGET="$TARGET" \
    python3 - <<PY
import json, os
from pathlib import Path
p = Path("$STATE")
data = {
  "anchor_epoch": int(os.environ["ANCHOR_EPOCH"]),
  "start_day": int("$START_DAY"),
  "end_day": int("$END_DAY"),
  "day_sec": int("$DAY_SEC"),
  "target": "$TARGET",
}
p.write_text(json.dumps(data, indent=2) + "\\n")
PY
  log "init anchor_epoch=$epoch (from ${first_sess:-now})"
}

current_day_and_deadline() {
  python3 - <<PY
import json, time
from pathlib import Path
state = json.loads(Path("$STATE").read_text())
anchor = int(state["anchor_epoch"])
day_sec = int(state.get("day_sec") or 86400)
start_day = int(state.get("start_day") or 1)
end_day = int(state.get("end_day") or 14)
now = int(time.time())
offset = max(0, now - anchor)
day_idx = offset // day_sec
day = start_day + int(day_idx)
deadline = anchor + (day_idx + 1) * day_sec
remaining = max(0, deadline - now)
print(day, deadline, remaining)
PY
}

day_published() {
  local day="$1"
  local html="$ROOT/web/site/reports/oss-cve-watch-libheif/day$(printf '%02d' "$day").html"
  [[ -f "$html" ]] || return 1
  grep -q 'NOT PUBLISHED YET' "$html" 2>/dev/null && return 1
  return 0
}

latest_day_out() {
  local day="$1"
  find "$WATCH_DIR" -maxdepth 1 -type d -name "day$(printf '%02d' "$day")-libfuzzer-*" \
    | while read -r d; do
        [[ -f "$d/ROLLUP.json" ]] || continue
        echo "$(stat -c '%Y' "$d/ROLLUP.json") $d"
      done | sort -nr | head -1 | cut -d' ' -f2-
}

finalize_day_out() {
  local day="$1"
  local out stamp sess
  out="$(latest_day_out "$day")"
  if [[ -z "$out" || ! -f "$out/ROLLUP.json" ]]; then
    stamp="$(ls -1td "$ROOT/reports/oss-cve-libfuzzer/$TARGET/sessions/"*"-d$(printf '%02d' "$day")"* 2>/dev/null | head -1 || \
             ls -1td "$ROOT/reports/oss-cve-libfuzzer/$TARGET/sessions/"* 2>/dev/null | head -1 || true)"
    if [[ -n "$stamp" && -f "$stamp/fuzzer.log" ]]; then
      bin="$ROOT/.cache/oss-cve-clones/libheif/build-fuzz-asan/fuzzing/file_fuzzer"
      [[ -x "$bin" ]] || bin="$ROOT/.cache/oss-libfuzzer-bin/libheif-file_fuzzer-asan"
      if [[ ! -f "$stamp/ROLLUP.json" && -x "$bin" ]]; then
        python3 "$ROOT/scripts/ops/export_oss_libfuzzer_session.py" "$TARGET" "$stamp" "$bin" >>"$LOG" 2>&1 || true
      fi
      out="$WATCH_DIR/day$(printf '%02d' "$day")-libfuzzer-$(basename "$stamp")"
      mkdir -p "$out"
      cp -f "$stamp/ROLLUP.json" "$out/ROLLUP.json" 2>/dev/null || true
      cp -f "$stamp/SESSION.json" "$out/SESSION.json" 2>/dev/null || true
      cp -f "$stamp/fuzzer.log" "$out/fuzzer.log" 2>/dev/null || true
    fi
  fi
  [[ -n "$out" && -f "$out/ROLLUP.json" ]] && echo "$out" >"$WATCH_DIR/.last_out_day$(printf '%02d' "$day")"
  echo "${out:-}"
}

publish_day() {
  local day="$1"
  if day_published "$day"; then
    log "day $day already published"
    return 0
  fi
  local out
  out="$(finalize_day_out "$day")"
  [[ -n "$out" && -f "$out/ROLLUP.json" ]] || {
    log "ERROR: cannot publish day $day — no ROLLUP"
    return 1
  }
  log "publish day $day from $out"
  DAY="$day" OUT="$out" NODE_SSH="$NODE_SSH" GIT_PUSH="$GIT_PUSH" \
    MIN_ITERATIONS="${MIN_ITERATIONS:-20000000}" MIN_ELAPSED_SEC="${MIN_ELAPSED_SEC:-82800}" \
    bash "$ROOT/scripts/ops/publish_oss_cve_watch_libheif_day_finish.sh" | tee -a "$LOG"
}

run_day_until() {
  local day="$1"
  local deadline_epoch="$2"
  local stamp out remaining
  stamp="$(date -u +%Y%m%dT%H%M%SZ)-d$(printf '%02d' "$day")"
  out="$WATCH_DIR/day$(printf '%02d' "$day")-libfuzzer-$stamp"
  mkdir -p "$out"

  log "=== DAY $day fuzz until $(date -u -d "@$deadline_epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -r "$deadline_epoch" +%Y-%m-%dT%H:%M:%SZ) ==="

  while true; do
    local now_epoch
    now_epoch="$(date +%s)"
    remaining=$((deadline_epoch - now_epoch))
    if [[ "$remaining" -le 30 ]]; then
      log "day $day deadline reached"
      stop_fuzzer_graceful
      break
    fi

    if [[ -f "$out/ROLLUP.json" ]]; then
      break
    fi

    local fp
    fp="$(fuzzer_pid)"
    if [[ -n "$fp" ]]; then
      sleep "$WATCHDOG_SEC"
      continue
    fi

    log "start/resume day $day remaining=${remaining}s (~$((remaining/3600))h)"
    (
      set +e
      DAY="$day" TARGET="$TARGET" MAX_TIME="$remaining" SKIP_REBUILD="$SKIP_REBUILD" \
        SKIP_PUBLISH=1 STAMP="$stamp" \
        bash "$ROOT/scripts/ops/run_oss_cve_watch_libheif_libfuzzer_day.sh" >>"$LOG" 2>&1
    ) &
    local wrap_pid=$!

    while kill -0 "$wrap_pid" 2>/dev/null; do
      now_epoch="$(date +%s)"
      if [[ "$now_epoch" -ge "$deadline_epoch" ]]; then
        log "deadline during day $day — graceful stop"
        stop_fuzzer_graceful
        wait "$wrap_pid" 2>/dev/null || true
        break 2
      fi
      sleep "$WATCHDOG_SEC"
      fp="$(fuzzer_pid)"
      if [[ -z "$fp" ]] && [[ ! -f "$out/ROLLUP.json" ]] && ! kill -0 "$wrap_pid" 2>/dev/null; then
        break
      fi
    done
    wait "$wrap_pid" 2>/dev/null || true

    if [[ -f "$out/ROLLUP.json" ]]; then
      break
    fi
    sess="$ROOT/reports/oss-cve-libfuzzer/$TARGET/sessions/$stamp"
    if [[ -f "$sess/ROLLUP.json" ]]; then
      cp "$sess/ROLLUP.json" "$out/ROLLUP.json"
      cp "$sess/SESSION.json" "$out/SESSION.json" 2>/dev/null || true
      break
    fi
    stamp="$(date -u +%Y%m%dT%H%M%SZ)-d$(printf '%02d' "$day")-r$(date +%s)"
    out="$WATCH_DIR/day$(printf '%02d' "$day")-libfuzzer-$stamp"
    mkdir -p "$out"
    log "retry day $day stamp=$stamp"
  done

  finalize_day_out "$day" >/dev/null || true
  [[ -f "$(latest_day_out "$day")/ROLLUP.json" ]] || {
    log "FATAL day $day no ROLLUP after deadline"
    return 1
  }
  log "day $day fuzz window complete"
}

{
  log "start TARGET=$TARGET DAY_SEC=$DAY_SEC START_DAY=$START_DAY END_DAY=$END_DAY"
  log "log=$LOG"
  load_or_init_state

  while true; do
    read -r day deadline remaining <<<"$(current_day_and_deadline)"
    if [[ "$day" -gt "$END_DAY" ]]; then
      log "series complete through day $END_DAY"
      break
    fi
    log "active day=$day deadline_epoch=$deadline remaining=${remaining}s (~$((remaining/3600))h)"

    if [[ "$remaining" -le 60 ]]; then
      stop_fuzzer_graceful
      sleep 5
      finalize_day_out "$day" >/dev/null || true
      if [[ "${SKIP_PUBLISH:-0}" != "1" ]]; then
        publish_day "$day" || log "WARN publish day $day failed"
      fi
      sleep 30
      continue
    fi

    if [[ -n "$(fuzzer_pid)" ]]; then
      log "adopt live fuzzer until day $day deadline"
      while [[ "$(date +%s)" -lt "$deadline" ]]; do
        [[ -n "$(fuzzer_pid)" ]] || break
        sleep "$WATCHDOG_SEC"
      done
      stop_fuzzer_graceful
      sleep 10
      finalize_day_out "$day" >/dev/null || true
      if [[ "$(date +%s)" -ge "$((deadline - 30))" ]]; then
        if [[ "${SKIP_PUBLISH:-0}" != "1" ]]; then
          publish_day "$day" || log "WARN publish day $day failed"
        fi
        sleep 30
        continue
      fi
    fi

    run_day_until "$day" "$deadline" || log "WARN run_day_until $day failed"
    finalize_day_out "$day" >/dev/null || true
    if [[ "${SKIP_PUBLISH:-0}" != "1" ]]; then
      publish_day "$day" || log "WARN publish day $day failed"
    fi
    sleep 10
  done
  log "ALL DONE libheif 24h cadence"
} 2>&1 | tee -a "$LOG"
