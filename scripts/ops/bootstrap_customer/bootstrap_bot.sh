#!/usr/bin/env bash
# Bootstrap customer bot — rotates OSS targets, spends wallet on pool audits.
# Cadence: timer every 6h ≈ 4 orders/day.
#
#   bash /opt/hackme-bootstrap/scripts/bootstrap_customer/bootstrap_bot.sh
#   BOOTSTRAP_DRY_RUN=1 bash ...  # wallet check only
#
# Defaults tuned for visible marketplace dwell (~10–20+ min at current workerfuzz
# throughput) and honest 20/80 escrow splits — not micro 96-run flashes.
set -euo pipefail
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE="${BASE:-http://127.0.0.1:8080}"
LOG="$INSTALL/logs/bootstrap/bot.log"
STATE="$INSTALL/logs/bootstrap/bot_state.json"
mkdir -p "$INSTALL/logs/bootstrap" "$(dirname "$STATE")"

TARGETS=(nghttp2 md4c cjson jsmn yyjson expat)
IDX=0
if [[ -f "$STATE" ]]; then
  IDX="$(python3 -c "import json; print(json.load(open('$STATE')).get('target_idx',0))" 2>/dev/null || echo 0)"
fi
TARGET="${TARGETS[$((IDX % ${#TARGETS[@]}))]}"

log() { echo "[bootstrap-bot $(date -u +%H:%M:%S)] $*" | tee -a "$LOG"; }

ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$INSTALL/.env" | cut -d= -f2- | tr -d '\r\n')"
bal="$(curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $ADMIN" "$BASE/api/wallet" | jq -r '.balance_orders_spendable_hmc // .balance_hmc // 0')"
log "wallet spendable_hmc=$bal target=$TARGET idx=$IDX"

MIN_BAL="${MIN_BALANCE_HMC:-8}"
if python3 -c "import sys; sys.exit(0 if float('$bal') >= float('$MIN_BAL') else 1)"; then
  :
else
  log "SKIP order — balance $bal < min $MIN_BAL (wait for mining settle or top-up)"
  exit 0
fi

# Keep a fat reserve; spend up to 12 HMC/order so 20% run-pool is meaningful.
RESERVE="${RESERVE_HMC:-80}"
budget="$(python3 -c "b=float('$bal'); r=float('$RESERVE'); print(round(min(12.0, max(6.0, min(12.0, b-r))), 4))")"
# ~25k–40k runs @ ~30 runs/s ≈ 15–20 min on marketplace; override with BUDGET_RUNS=.
runs="${BUDGET_RUNS:-32000}"
solves="${TARGET_SOLVES:-8}"
# Scale runs lightly with budget so per_run_hmc stays readable (~0.00005–0.0001).
if [[ -z "${BUDGET_RUNS:-}" ]]; then
  runs="$(python3 -c "b=float('$budget'); print(int(max(16000, min(48000, round((b*0.20)/0.00008)))) )")"
fi

if [[ "${BOOTSTRAP_DRY_RUN:-0}" == "1" ]]; then
  log "DRY_RUN would place target=$TARGET budget=$budget runs=$runs solves=$solves"
  exit 0
fi

export BOOTSTRAP_INSTALL="$INSTALL" BUDGET_HMC="$budget" BUDGET_RUNS="$runs" REWARD_HMC="${REWARD_HMC:-0.05}" TARGET_SOLVES="$solves"
export POLL_SEC="${POLL_SEC:-90}" MAX_WAIT="${MAX_WAIT:-21600}"
# Solvable PoH gate so attach can move progress_count (fuzz budget is separate).
export HACKME_MINIMAL_POH_GATE="${HACKME_MINIMAL_POH_GATE:-1}"
log "placing target=$TARGET budget_hmc=$budget runs=$runs solves=$solves poh_gate=minimal"
bash "$SCRIPT_DIR/place_bootstrap_order.sh" "$TARGET" >>"$LOG" 2>&1

python3 -c "
import json, pathlib, time
p = pathlib.Path('$STATE')
st = json.loads(p.read_text()) if p.exists() else {}
st['target_idx'] = ($IDX + 1) % ${#TARGETS[@]}
st['last_target'] = '$TARGET'
st['last_budget_hmc'] = float('$budget')
st['last_budget_runs'] = int('$runs')
st['last_run_utc'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
st['cadence'] = '4_per_day_deep'
p.write_text(json.dumps(st, indent=2) + '\n')
"
log "next target_idx=$(( (IDX + 1) % ${#TARGETS[@]} ))"
