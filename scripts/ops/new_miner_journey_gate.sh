#!/usr/bin/env bash
# End-to-end gate: "fresh miner downloaded from site" — network, public APIs, coordinator, optional worker smoke.
#
# Run from repo root on the machine you want to test (home PC or new VPS):
#   bash scripts/ops/new_miner_journey_gate.sh
#
# Remote-city simulation (second machine):
#   WORKER_ID=worker-vps-msk-01 WORKER_SMOKE_SEC=90 bash scripts/ops/new_miner_journey_gate.sh
#
# Long network soak (optional, separate):
#   BASE=https://hackme.tech DURATION_SEC=1800 bash scripts/ops/network_stability_soak.sh
#
# Exit: 0 = pass (or skip worker smoke), 1 = hard fail on connectivity/APIs.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "[new-miner-gate] missing: $1" >&2; exit 1; }; }
require_cmd curl
require_cmd jq

BASE="${BASE:-https://hackme.tech}"
COORD_URL="${COORD_URL:-${BASE%/}/pool/coordinator}"
WORKER_SMOKE="${WORKER_SMOKE:-1}"
WORKER_SMOKE_SEC="${WORKER_SMOKE_SEC:-60}"
WORKER_ID="${WORKER_ID:-new-miner-$(hostname -s 2>/dev/null || echo test)-$(date -u +%Y%m%d)}"
RUN_ID="${RUN_ID:-new_miner_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/new-miner-$RUN_ID}"
mkdir -p "$OUT_DIR"

REPORT="$OUT_DIR/report.txt"
: >"$REPORT"

log() { echo "$*" | tee -a "$REPORT"; }
fail() { log "FAIL: $*"; exit 1; }
warn() { log "WARN: $*"; }

probe_http() {
  local name="$1"
  local url="$2"
  local extra_args="${3:-}"
  local code lat body
  # shellcheck disable=SC2086
  read -r code lat body < <(
    curl -sS -o "$OUT_DIR/body_${name}.json" -w '%{http_code} %{time_total}' --max-time 20 $extra_args "$url" 2>/dev/null || echo "000 99"
  )
  lat_ms="$(awk -v t="$lat" 'BEGIN { printf "%.0f", t * 1000 }')"
  log "  $name  HTTP $code  ${lat_ms}ms  $url"
  if [[ "$code" != "200" ]]; then
    return 1
  fi
  return 0
}

log "=== HackMe new-miner journey gate ==="
log "host=$(hostname -f 2>/dev/null || hostname) run_id=$RUN_ID"
log "base=$BASE coord=$COORD_URL worker_id=$WORKER_ID"
log ""

log "--- 1) ICMP / DNS (best-effort) ---"
host="${BASE#https://}"
host="${host#http://}"
host="${host%%/*}"
if command -v getent >/dev/null 2>&1; then
  ip="$(getent ahosts "$host" 2>/dev/null | awk '/STREAM/ {print $1; exit}')"
  log "  DNS $host -> ${ip:-?}"
fi
if command -v ping >/dev/null 2>&1; then
  if ping -c 3 -W 2 "$host" 2>/dev/null | tee -a "$REPORT" | tail -1 | grep -q 'min/avg/max'; then
    :
  else
    warn "ping to $host failed or blocked (ICMP often disabled on VPS) — HTTPS probes below matter more"
  fi
else
  warn "ping not installed"
fi
log ""

log "--- 2) Public stack (as in browser / first open) ---"
probe_http "site_root" "${BASE%/}/" || fail "site root unreachable"
probe_http "api_status" "${BASE%/}/api/status" || fail "public /api/status failed"
probe_http "api_global" "${BASE%/}/api/global/metrics" || warn "global metrics failed (non-fatal)"
probe_http "coord_work_stats" "${COORD_URL%/}/api/work/stats" || fail "coordinator work/stats failed"
probe_http "coord_work_details" "${COORD_URL%/}/api/work/stats?details=1" || warn "work/stats?details=1 failed"

st="$(jq -c '{tip_height,canonical_tip_height,network_mode_active,pool_coordinator_url_effective:(.pool_coordinator_url_effective//.pool_coordinator_url//"")}' "$OUT_DIR/body_api_status.json" 2>/dev/null || echo '{}')"
log "  status snapshot: $st"
ws="$(jq -c '{workers_count,accepted_attempts,submitted_items,hybrid_signer_enabled:(.hybrid_signer_enabled//false)}' "$OUT_DIR/body_coord_work_details.json" 2>/dev/null || echo '{}')"
log "  coordinator: $ws"
log ""

log "--- 3) Latency budget (3 samples) ---"
for i in 1 2 3; do
  lat="$(curl -sS -o /dev/null -w '%{time_total}' --max-time 15 "${COORD_URL%/}/api/work/stats" || echo 9)"
  ms="$(awk -v t="$lat" 'BEGIN { printf "%.0f", t * 1000 }')"
  log "  coord sample $i: ${ms}ms"
done
log "  target: stable < 500ms from your region; spikes > 2s may cause claim timeouts"
log ""

log "--- 4) Desktop path (participant with local dashboard) ---"
log "  Simulates: download -> .env.desktop -> desktop_mode_up -> Start worker"
if [[ -f "$ROOT_DIR/.env.desktop" ]] && curl -fsS --max-time 5 http://127.0.0.1:8080/api/status >/dev/null 2>&1; then
  loc="$(curl -fsS --max-time 8 http://127.0.0.1:8080/api/worker/status 2>/dev/null | jq -c '{running,worker_id,measured_hashrate_gh_s,external_worker}' 2>/dev/null || echo '{}')"
  log "  local dashboard: $loc"
else
  log "  local dashboard: not running (skip — normal on fresh VPS; use worker_vps_deploy.sh there)"
fi
log ""

log "--- 5) Pool worker smoke (hybrid sign -> coordinator) ---"
if [[ "$WORKER_SMOKE" == "1" ]]; then
  export COORD_URL WORKER_ID PUBLIC_WORKER_SMOKE_SEC="$WORKER_SMOKE_SEC"
  if bash "$ROOT_DIR/scripts/ops/run_public_worker_smoke.sh" 2>&1 | tee -a "$REPORT"; then
    log "  worker smoke: PASS"
  else
    ec=$?
    if [[ "$ec" -eq 0 ]]; then
      log "  worker smoke: SKIP (no secrets)"
    else
      fail "worker smoke failed — check coordinator token + miner seed in .secrets/"
    fi
  fi
else
  log "  worker smoke: skipped (WORKER_SMOKE=0)"
fi
log ""

log "--- 6) Post-smoke: worker visible on coordinator? ---"
sleep 2
if probe_http "coord_after" "${COORD_URL%/}/api/work/stats?details=1"; then
  row="$(jq -c --arg w "$WORKER_ID" '.workers[$w] // .workers["worker-active"] // empty' "$OUT_DIR/body_coord_after.json" 2>/dev/null || echo 'null')"
  if [[ "$row" != "null" && -n "$row" ]]; then
    log "  worker row: $row"
  else
    keys="$(jq -r '(.workers // {}) | keys | join(", ")' "$OUT_DIR/body_coord_after.json" 2>/dev/null || echo '')"
    warn "worker $WORKER_ID not in workers{} yet (keys: $keys) — wait 1–2 min or check WORKER_ID / signing"
  fi
fi
log ""

log "=== PASS: new-miner journey gate ==="
log "Report: $REPORT"
log ""
log "Next steps for 'another city' VPS:"
log "  WORKER_SSH=root@NEW_IP WORKER_ID=worker-vps-01 bash scripts/ops/worker_vps_deploy.sh"
log "  Add WORKER_PAYOUT_MAP on hackme.tech for settlement to your HMC address."
