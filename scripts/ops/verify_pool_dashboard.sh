#!/usr/bin/env bash
# Smoke-check pool APIs + calculator sanity while mining is active.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
DESKTOP_BASE="${DESKTOP_BASE:-http://127.0.0.1:8080}"
TOKEN_FILE="${TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_admin_token}"
TOKEN="$(tr -d '\r\n' <"$TOKEN_FILE")"

log() { echo "[verify-pool] $*" >&2; }
fail=0

check_json() {
  local name="$1" url="$2" hdr="${3:-}" out
  if [[ -n "$hdr" ]]; then
    out="$(curl -fsS -H "$hdr" "$url" 2>/dev/null)" || { log "FAIL $name: curl $url"; fail=1; echo '{}'; return; }
  else
    out="$(curl -fsS "$url" 2>/dev/null)" || { log "FAIL $name: curl $url"; fail=1; echo '{}'; return; }
  fi
  echo "$out" | jq -e . >/dev/null 2>&1 || { log "FAIL $name: invalid json"; fail=1; echo '{}'; return; }
  log "OK $name"
  echo "$out"
}

log "=== coordinator work/stats ==="
WS="$(check_json work_stats "$COORD_URL/api/work/stats?details=1" "X-Hackme-Admin-Token: $TOKEN")"
M=$(echo "$WS" | jq -r '.target_mod // 0')
MAX_M=$(echo "$WS" | jq -r '.target_mod_max // 1000000000')
POOL_GH="$(echo "$WS" | jq -r '.pool_hashrate_gh_s // 0')"
RPM="$(echo "$WS" | jq -r '.reward_per_m // 0')"
ATT="$(echo "$WS" | jq -r '.accepted_attempts // 0')"
PAYOUT="$(echo "$WS" | jq -r '.total_payout_hmc // 0')"
log "  M=$M max_M=$MAX_M pool_gh=$POOL_GH reward/M=$RPM attempts=$ATT payout=$PAYOUT"
echo "$WS" | jq -r '.workers | to_entries[] | "  \(.key): ghs=\(.value.hashrate_gh_s // 0) payout=\(.value.payout_hmc // 0)"'

log "=== desktop status + metrics ==="
ST="$(check_json desktop_status "$DESKTOP_BASE/api/status")"
echo "$ST" | jq -c '{mining,tip_height,canonical_tip_height,network_mode_active,chain_leader_local_poh}'
MET="$(check_json desktop_metrics "$DESKTOP_BASE/api/metrics" "X-Hackme-Admin-Token: $(grep -m1 HACKME_ADMIN_TOKEN "$ROOT/.env.desktop" 2>/dev/null | cut -d= -f2 || true)")"
WST="$(check_json worker_status "$DESKTOP_BASE/api/worker/status" 2>/dev/null || echo '{}')"
echo "$WST" | jq -c '{running,worker_id,measured_hashrate_gh_s}' 2>/dev/null || true

log "=== calculator sanity (pool cap + live payout) ==="
DESKTOP_WS="$(curl -fsS "$DESKTOP_BASE/api/work/stats" 2>/dev/null || echo '{}')"
WST="$(curl -fsS "$DESKTOP_BASE/api/worker/status" 2>/dev/null || echo '{}')"
WORKER_GH="$(echo "$WST" | jq -r '.measured_hashrate_gh_s // 0')"
python3 - "$M" "$POOL_GH" "$RPM" "$WORKER_GH" "$MAX_M" <<'PY'
import sys
M, pool_gh, rpm, worker_gh, max_m = map(float, sys.argv[1:6])
user_gh = 20.0
eff = min(user_gh, pool_gh) if pool_gh > 0 else user_gh
formula_h = (eff * 1e9 / 1e6) * rpm * 3600
# Live payout ~0.006/min from dashboard is realistic band
print(f"  pool M={M:.0f} max_M={max_m:.0f} pool_gh={pool_gh:.2f} worker_gh={worker_gh:.2f}")
print(f"  formula@20GH/s (capped eff {eff:.2f}) ≈ {formula_h:.2f} HMC/h")
if formula_h > 500:
    print("  WARN: formula still high — UI should prefer payout Δ/min (live) as primary")
else:
    print("  PASS: formula in sane band")
if M < 2_000_000:
    print("  FAIL: M below pool floor")
    sys.exit(2)
if max_m > 0 and M >= max_m * 0.995:
    print(f"  WARN: M at/near configured max cap ({max_m:.0f}) — raise HACKME_COORDINATOR_POOL_TARGET_MOD_MAX")
print("  PASS: M in pool range")
PY

log "=== VPS chain ==="
ssh -o BatchMode=yes -o ConnectTimeout=10 hackme-vps \
  'curl -fsS http://127.0.0.1:18080/api/status | jq -c "{tip_height,mining}"' || { log "WARN: VPS status"; fail=1; }

if [[ "$fail" -ne 0 ]]; then
  log "DONE with failures"
  exit 1
fi
log "DONE all checks passed"
exit 0
