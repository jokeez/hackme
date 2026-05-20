#!/usr/bin/env bash
# Live pool + node health snapshot (operator cron / manual).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
DESKTOP_BASE="${DESKTOP_BASE:-http://127.0.0.1:8080}"
TOKEN_FILE="${TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_admin_token}"
ADMIN_FILE="${ADMIN_FILE:-$ROOT/.env.desktop}"
TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

log() { echo "[$TS] $*"; }

TOKEN=""
[[ -f "$TOKEN_FILE" ]] && TOKEN="$(tr -d '\r\n' <"$TOKEN_FILE")"
ADMIN=""
[[ -f "$ADMIN_FILE" ]] && ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$ADMIN_FILE" 2>/dev/null | cut -d= -f2- || true)"

HDR=()
[[ -n "$TOKEN" ]] && HDR+=(-H "X-Hackme-Admin-Token: $TOKEN")
WS="$(curl -fsS "${HDR[@]}" "$COORD_URL/api/work/stats?details=1" 2>/dev/null || echo '{}')"

python3 - "$WS" "$COORD_URL" <<'PY'
import json, sys
ws = json.loads(sys.argv[1] or "{}")
base = sys.argv[2]
m = ws.get("target_mod") or 0
pool_gh = float(ws.get("pool_hashrate_gh_s") or 0)
rpm = float(ws.get("reward_per_m") or 0)
workers = ws.get("workers") or {}
print(f"pool M={m:,} pool_gh={pool_gh:.2f} reward/M={rpm:.8f} workers={len(workers)}")
for wid in sorted(workers):
    w = workers[wid] or {}
    gh = float(w.get("hashrate_gh_s") or 0)
    pay = float(w.get("payout_hmc") or 0)
    print(f"  {wid}: {gh:.3f} GH/s payout={pay:.6f} HMC")
print(f"public: {base}/api/pool/stats")
PY

curl -fsS "${COORD_URL}/api/pool/stats" 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin)
print(f\"pool/stats: miners={d.get('miners')} hashrate_hs={d.get('hashrate_hs',0)/1e9:.2f} GH/s status={d.get('status')}\")
" 2>/dev/null || log "WARN: pool/stats unavailable"

if [[ -n "$ADMIN" ]]; then
  curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" "$DESKTOP_BASE/api/worker/status" 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin)
print(f\"local worker: running={d.get('running')} id={d.get('worker_id')} measured_gh={d.get('measured_hashrate_gh_s')}\")
" 2>/dev/null || log "WARN: local worker status"
fi

curl -fsSI "https://hackme.tech/dist/release_0.1.0-rc11e/HackMe-Setup-0.1.0-rc11e.exe" 2>/dev/null | head -1 || log "WARN: installer HEAD failed"
log "DONE"
