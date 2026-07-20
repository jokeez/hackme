#!/usr/bin/env bash
# One-shot: libheif 24h cadence + file_fuzzer health. Restarts cadence if dead outside publish window.
set -euo pipefail
ROOT="${HACKME_ROOT:-/opt/hackme}"
LOG="$ROOT/logs/oss-cve-libheif-watchdog.log"
mkdir -p "$ROOT/logs"

ts() { date -u +%Y-%m-%dT%H:%M:%SZ; }
log() { echo "[libheif-watchdog $(ts)] $*" | tee -a "$LOG" >/dev/null; }

TARGET="${TARGET:-libheif}"
cadence_active=0
systemctl is-active --quiet hackme-libheif-24h.service 2>/dev/null && cadence_active=1

fp="$(pgrep -f "file_fuzzer.*oss-cve-libfuzzer/${TARGET}/corpus" | head -1 || true)"
wrap="$(pgrep -f 'run_oss_cve_watch_libheif_24h_cadence|run_oss_cve_watch_libheif_libfuzzer_day' | head -1 || true)"

day="$(python3 - "$ROOT/reports/oss-cve-watch-libheif/cadence.json" 2>/dev/null <<'PY' || echo 0
import json, sys, time
from pathlib import Path
p = Path(sys.argv[1])
if not p.is_file():
    print(0); raise SystemExit
s = json.loads(p.read_text())
anchor = int(s.get("anchor_epoch") or 0)
day_sec = int(s.get("day_sec") or 86400)
start = int(s.get("start_day") or 1)
end = int(s.get("end_day") or 14)
now = int(time.time())
day = start + max(0, now - anchor) // day_sec
print(min(day, end + 1))
PY
)"

published=0
[[ -f "$ROOT/web/site/reports/oss-cve-watch-libheif/day$(printf '%02d' "$day").html" ]] && published=1

if [[ "$day" -gt 14 ]]; then
  exit 0
fi

if [[ "$cadence_active" -eq 0 ]]; then
  log "WARN cadence inactive day=$day — starting hackme-libheif-24h.service"
  systemctl start hackme-libheif-24h.service || log "ERROR start cadence failed"
  exit 0
fi

if [[ -n "$fp" ]]; then
  exit 0
fi

if [[ -n "$wrap" ]]; then
  exit 0
fi

log "WARN fuzzer dead day=$day published=$published — cadence should recover; nudging restart"
systemctl restart hackme-libheif-24h.service || true
