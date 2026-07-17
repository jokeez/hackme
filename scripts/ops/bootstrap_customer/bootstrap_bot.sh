#!/usr/bin/env bash
# Bootstrap customer bot — rotates OSS targets, spends wallet on pool audits.
# Cadence: timer every 6h ≈ 4 orders/day.
#
#   bash /opt/hackme-bootstrap/scripts/bootstrap_customer/bootstrap_bot.sh
#   BOOTSTRAP_DRY_RUN=1 bash ...  # wallet check only
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

MIN_BAL="${MIN_BALANCE_HMC:-3}"
if python3 -c "import sys; sys.exit(0 if float('$bal') >= float('$MIN_BAL') else 1)"; then
  :
else
  log "SKIP order — balance $bal < min $MIN_BAL (wait for mining settle or top-up)"
  exit 0
fi

# 4/day cadence: modest budgets so spendable wallet lasts
RESERVE="${RESERVE_HMC:-20}"
budget="$(python3 -c "b=float('$bal'); r=float('$RESERVE'); print(round(min(4.0, max(1.5, b-r)), 4))")"
runs="${BUDGET_RUNS:-96}"
solves="${TARGET_SOLVES:-4}"

if [[ "${BOOTSTRAP_DRY_RUN:-0}" == "1" ]]; then
  log "DRY_RUN would place target=$TARGET budget=$budget runs=$runs"
  exit 0
fi

export BOOTSTRAP_INSTALL="$INSTALL" BUDGET_HMC="$budget" BUDGET_RUNS="$runs" REWARD_HMC=0.05 TARGET_SOLVES="$solves"
export POLL_SEC="${POLL_SEC:-60}" MAX_WAIT="${MAX_WAIT:-14400}"
# Prefer solvable PoH gate so progress_count moves (fuzz budget is separate).
export HACKME_MINIMAL_POH_GATE="${HACKME_MINIMAL_POH_GATE:-0}"
bash "$SCRIPT_DIR/place_bootstrap_order.sh" "$TARGET" >>"$LOG" 2>&1

python3 -c "
import json, pathlib, time
p = pathlib.Path('$STATE')
st = json.loads(p.read_text()) if p.exists() else {}
st['target_idx'] = ($IDX + 1) % ${#TARGETS[@]}
st['last_target'] = '$TARGET'
st['last_run_utc'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
st['cadence'] = '4_per_day'
p.write_text(json.dumps(st, indent=2) + '\n')
"
log "next target_idx=$(( (IDX + 1) % ${#TARGETS[@]} ))"
