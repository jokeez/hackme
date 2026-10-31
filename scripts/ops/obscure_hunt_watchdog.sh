#!/usr/bin/env bash
# Obscure hunt watchdog — ensure continuous local ASAN loop stays up.
# Invoked by user systemd: hackme-obscure-watchdog.service / timer.
set -euo pipefail

UNIT="${OBSCURE_HUNT_UNIT:-hackme-obscure-hunt.service}"
log() { echo "[obscure-watchdog $(date -u +%H:%M:%S)] $*"; }

st="$(systemctl --user is-active "$UNIT" 2>/dev/null || echo unknown)"
if [[ "$st" == "active" ]]; then
  # Also require the loop process to be present (unit can be active but empty).
  if pgrep -f 'run_oss_cve_obscure_loop\.sh' >/dev/null 2>&1; then
    log "ok unit=$UNIT loop=running"
    exit 0
  fi
  log "WARN unit=$UNIT active but loop process missing — restarting"
  systemctl --user restart "$UNIT"
  exit 0
fi

log "WARN unit=$UNIT state=$st — restarting"
systemctl --user restart "$UNIT" || {
  log "ERROR failed to restart $UNIT"
  exit 1
}
sleep 2
st2="$(systemctl --user is-active "$UNIT" 2>/dev/null || echo unknown)"
log "after restart state=$st2"
[[ "$st2" == "active" ]]
