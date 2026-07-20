#!/usr/bin/env bash
# One-shot watchdog: log if cadence is active but fuzzer vanished outside the
# intentional 15:00–22:00 idle window. Does NOT restart the cadence service
# (that races with in-script recovery); only alerts via log.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG="$ROOT/logs/oss-cve-fuzzer-watchdog.log"
STATUS="$ROOT/logs/oss-cve-fuzzer-watchdog.status"
mkdir -p "$ROOT/logs"

ts() { date -Is; }
log() { echo "[watchdog $(ts)] $*" | tee -a "$LOG" >/dev/null; }

hour="$(date +%H)"
fp="$(pgrep -f '/\.cache/oss-libfuzzer-bin/nghttp2-asan-fuzzer' || true)"
wrap="$(pgrep -f 'run_oss_cve_watch_evening_cadence|run_oss_libfuzzer_session|run_oss_cve_watch_libfuzzer_day|run_oss_cve_watch_day_autopublish' || true)"
cadence_active=0
systemctl --user is-active --quiet hackme-cve-evening.service 2>/dev/null && cadence_active=1

{
  echo "ts=$(ts)"
  echo "hour=$hour"
  echo "cadence_active=$cadence_active"
  echo "fuzzer_pids=${fp:-none}"
  echo "wrapper_pids=${wrap:-none}"
} >"$STATUS"

# Intentional idle after until-hour before evening publish
if [[ "$hour" -ge 15 && "$hour" -lt 22 ]]; then
  exit 0
fi

if [[ "$cadence_active" -eq 0 ]]; then
  # Prefer cadence always running until day14 done
  if [[ ! -f "$ROOT/web/site/reports/oss-cve-watch/day14.html" ]]; then
    log "WARN cadence inactive and day14 not published — starting hackme-cve-evening"
    systemctl --user start hackme-cve-evening.service || true
  fi
  exit 0
fi

if [[ -n "$fp" ]]; then
  exit 0
fi

# Fuzzer dead; wrappers still exporting is OK
if [[ -n "$wrap" ]]; then
  exit 0
fi

log "WARN fuzzer+wrappers dead while cadence active (hour=$hour) — cadence internal loop should recover"
exit 0
