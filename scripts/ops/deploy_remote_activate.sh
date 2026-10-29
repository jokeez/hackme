#!/usr/bin/env bash
# Remote half of deploy_hackme_node.sh — runs ON the VPS after rsync shipped a clean build.
# Installs units, restarts services, then gates on real chain progress and rolls the binaries
# back to the pre-deploy snapshot if the new build does not produce blocks.
#
#   deploy_remote_activate.sh <deploy_dir> <release_snapshot_dir> <commit> <version>
#
# Env: SKIP_FLEET_RESTART=1, SKIP_HEALTH_GATE=1, GATE_MIN_BLOCKS (2), GATE_BLOCK_SEC (300),
#      GATE_READY_SEC (90), STATUS_URL, COORD_HEALTH_URL.
set -euo pipefail

d="${1:?deploy dir}"
snap="${2:?release snapshot dir}"
commit="${3:-unknown}"
version="${4:-unknown}"

GATE_MIN_BLOCKS="${GATE_MIN_BLOCKS:-2}"
GATE_BLOCK_SEC="${GATE_BLOCK_SEC:-300}"
GATE_READY_SEC="${GATE_READY_SEC:-90}"
STATUS_URL="${STATUS_URL:-http://127.0.0.1:18080/api/status}"
COORD_HEALTH_URL="${COORD_HEALTH_URL:-http://127.0.0.1:18081/health}"
# /health answers without the work-manager lock; work/stats takes it — a deadlocked pool
# (2026-10-28: leaked m.mu in claim) passed /health while no miner could claim.
COORD_STATS_URL="${COORD_STATS_URL:-http://127.0.0.1:18081/api/work/stats}"
FLEET_UNITS=(hackme-workerfuzz-hub-fuzz-02 hackme-workerfuzz-hub-fuzz-03 hackme-workerfuzz-hub-fuzz-04)
# Binaries snapshotted before rsync and restored on rollback (paths relative to $d).
BINARIES=(hackme-node coordinator minersign bin/workersettle bin/workersettlectl bin/workerfuzz bin/hackme-wasm-check bin/checkpoint)

log() { echo "[deploy-activate] $*"; }

tip_height() {
  curl -fsS --max-time 10 "$STATUS_URL" 2>/dev/null | jq -er '.tip_height' 2>/dev/null || echo ""
}

restart_services() {
  systemctl restart hackme-node
  systemctl restart hackme-coordinator 2>/dev/null || true
  if [[ "${SKIP_FLEET_RESTART:-0}" != "1" ]]; then
    for u in "${FLEET_UNITS[@]}"; do
      systemctl restart "$u" 2>/dev/null || true
    done
  else
    log "SKIP_FLEET_RESTART=1 — left named hub-fuzz workers running"
  fi
}

# health_gate <tip_before> <since_epoch> → 0 healthy, 1 not.
health_gate() {
  local tip0="$1" since="$2" deadline tip
  deadline=$(( $(date +%s) + GATE_READY_SEC ))
  until tip="$(tip_height)"; [[ -n "$tip" ]]; do
    if (( $(date +%s) >= deadline )); then
      log "GATE FAIL: $STATUS_URL not answering after ${GATE_READY_SEC}s"
      return 1
    fi
    sleep 3
  done
  if ! systemctl is-active --quiet hackme-node; then
    log "GATE FAIL: hackme-node not active"
    return 1
  fi
  if systemctl is-enabled hackme-coordinator >/dev/null 2>&1; then
    if ! systemctl is-active --quiet hackme-coordinator; then
      log "GATE FAIL: hackme-coordinator not active"
      return 1
    fi
    # Coordinator loads work-dedup before /health listens (can take >5s on large DBs).
    deadline=$(( $(date +%s) + GATE_READY_SEC ))
    until curl -fsS --max-time 5 "$COORD_HEALTH_URL" >/dev/null 2>&1; do
      if (( $(date +%s) >= deadline )); then
        log "GATE FAIL: coordinator health $COORD_HEALTH_URL"
        return 1
      fi
      sleep 2
    done
  fi
  local want=$(( tip0 + GATE_MIN_BLOCKS ))
  deadline=$(( $(date +%s) + GATE_BLOCK_SEC ))
  log "gate: waiting for tip >= $want (was $tip0) within ${GATE_BLOCK_SEC}s"
  while :; do
    # A broken wasm validator rejects every PoH candidate (2026-10-27 halt) — fail fast.
    if journalctl -u hackme-node --since "@$since" --no-pager -o cat 2>/dev/null | grep -q "wasm gate rejecting"; then
      log "GATE FAIL: node journals 'wasm gate rejecting' — PoH candidates are being rejected"
      return 1
    fi
    tip="$(tip_height)"
    if [[ -n "$tip" ]] && (( tip >= want )); then
      if systemctl is-enabled hackme-coordinator >/dev/null 2>&1 &&
        ! curl -fsS --max-time 10 "$COORD_STATS_URL" >/dev/null 2>&1; then
        log "GATE FAIL: coordinator work manager not answering $COORD_STATS_URL (pool stuck)"
        return 1
      fi
      log "gate OK: tip $tip0 -> $tip, coordinator work manager answering"
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      log "GATE FAIL: tip ${tip:-?} did not reach $want in ${GATE_BLOCK_SEC}s (chain not producing blocks)"
      return 1
    fi
    sleep 5
  done
}

rollback() {
  log "ROLLBACK: restoring binaries from $snap"
  local b
  for b in "${BINARIES[@]}"; do
    if [[ -f "$snap/$b" ]]; then
      install -D -m 755 "$snap/$b" "$d/$b.rollback"
      mv -f "$d/$b.rollback" "$d/$b"
    fi
  done
  restart_services
  local prev="previous build" tip="" deadline
  [[ -f "$snap/DEPLOYED" ]] && prev="$(tr '\n' ' ' <"$snap/DEPLOYED")"
  deadline=$(( $(date +%s) + GATE_READY_SEC ))
  until tip="$(tip_height)"; [[ -n "$tip" ]] || (( $(date +%s) >= deadline )); do
    sleep 3
  done
  if [[ -z "$tip" ]]; then
    log "CRITICAL: node still not answering after rollback — the cause is not the binary; manual action needed"
    journalctl -u hackme-node -n 15 --no-pager -o cat 2>/dev/null | sed 's/^/    /' || true
    return 1
  fi
  log "ROLLED BACK to ${prev}; node answering, tip=${tip}"
}

# The services run as an unprivileged user: the deploy dir must stay traversable whatever
# mode rsync carried over from the build tree (a 0700 root caused status=200/CHDIR).
chmod 755 "$d"
if ! runuser -u hackme -- test -r "$d/.env.vps" -a -x "$d" 2>/dev/null && id hackme >/dev/null 2>&1; then
  log "FATAL: user hackme cannot read $d — fix permissions before restarting"
  exit 4
fi
if [[ ! -f "$d/.env.vps" ]]; then
  log "FATAL: $d/.env.vps missing on remote — recreate it before deploying"
  exit 3
fi
for b in "${BINARIES[@]}"; do
  [[ -f "$d/$b" ]] && chmod 755 "$d/$b"
done
# A missing HACKME_WASM_VALIDATE_BIN makes every PoH gate candidate fail → chain halts silently.
for envf in "$d/.env.vps" "$d/.env.coord"; do
  [[ -f "$envf" ]] || continue
  vbin="$(grep -m1 '^HACKME_WASM_VALIDATE_BIN=' "$envf" | cut -d= -f2- | tr -d '\r\n"' || true)"
  if [[ -n "$vbin" && ! -x "$vbin" ]]; then
    log "FATAL: $(basename "$envf") HACKME_WASM_VALIDATE_BIN=$vbin not executable — refusing restart"
    exit 5
  fi
done

for u in hackme-node.service hackme-coordinator.service hackme-node-watchdog.service hackme-node-watchdog.timer \
  hackme-oss-cve-nightly.service hackme-oss-cve-nightly.timer hackme-checkpoint.service hackme-checkpoint.timer; do
  if [[ -f "$d/scripts/ops/systemd/$u" ]]; then
    cp -a "$d/scripts/ops/systemd/$u" "/etc/systemd/system/$u"
  fi
done
chmod +x "$d/scripts/ops/node_healthcheck.sh" "$d/scripts/ops/run_oss_cve_nightly.sh" \
  "$d/scripts/ops/checkpoint_publish.sh" "$d/scripts/ops/workerfuzz_digger.sh" 2>/dev/null || true
systemctl daemon-reload
systemctl enable --now hackme-node-watchdog.timer 2>/dev/null || true
systemctl enable --now hackme-oss-cve-nightly.timer 2>/dev/null || true
# Checkpoints publish from a clone outside the deploy dir; enable only once it is provisioned.
if [[ -d /opt/hackme-checkpoints/.git ]]; then
  systemctl enable --now hackme-checkpoint.timer 2>/dev/null || true
fi
systemctl enable hackme-coordinator 2>/dev/null || true

# ORDERS attach / solve-relay must use the chain admin token (POST /api/tasks auth).
if [[ -f "$d/.env.vps" && -f "$d/.env.coord" ]]; then
  admin="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$d/.env.vps" | cut -d= -f2- | tr -d '\r\n' || true)"
  if [[ -n "$admin" ]]; then
    if grep -q '^HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN=' "$d/.env.coord"; then
      sed -i "s|^HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN=.*|HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN=${admin}|" "$d/.env.coord"
    else
      echo "HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN=${admin}" >>"$d/.env.coord"
    fi
    log "synced HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN from .env.vps"
  fi
fi

tip0="$(tip_height)"
since="$(date +%s)"
log "restarting (commit=$commit version=$version, tip before=${tip0:-?})"
restart_services

if [[ "${SKIP_HEALTH_GATE:-0}" == "1" ]]; then
  log "SKIP_HEALTH_GATE=1 — no chain-progress gate, no automatic rollback"
else
  if [[ -z "$tip0" ]]; then
    # Node was already down: progress can't be measured, gate on API + services coming up.
    log "WARN: no tip before restart (node was down?) — gating on liveness only"
    tip0=0
  fi
  if ! health_gate "$tip0" "$since"; then
    journalctl -u hackme-node --since "@$since" -n 15 --no-pager -o cat 2>/dev/null | sed 's/^/    /' || true
    rollback || exit 7
    exit 6
  fi
fi

printf 'commit=%s\nversion=%s\ndeployed_unix=%s\n' "$commit" "$version" "$(date +%s)" >"$d/DEPLOYED"
log "OK: commit=$commit version=$version tip=$(tip_height)"
