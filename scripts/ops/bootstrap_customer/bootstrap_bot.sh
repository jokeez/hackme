#!/usr/bin/env bash
# Bootstrap customer bot — rotates light OSS parser targets onto the public pool.
#
# Cadence (timer): ~2–3 orders/day for a bounded window (default 3 days).
#   bash /opt/hackme-bootstrap/scripts/bootstrap_customer/bootstrap_bot.sh
#   BOOTSTRAP_DRY_RUN=1 bash ...  # wallet check only
set -euo pipefail
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE="${BASE:-http://127.0.0.1:8080}"
LOG="$INSTALL/logs/bootstrap/bot.log"
STATE="$INSTALL/logs/bootstrap/bot_state.json"
PAYOUT_LOG="$INSTALL/logs/bootstrap/payout_track.jsonl"
mkdir -p "$INSTALL/logs/bootstrap" "$(dirname "$STATE")"

# Light parsers only (no heavy ASAN museum targets).
TARGETS=(jsmn yyjson cjson md4c nghttp2 expat)
IDX=0
PLAN_UNTIL=""
if [[ -f "$STATE" ]]; then
  IDX="$(python3 -c "import json; print(json.load(open('$STATE')).get('target_idx',0))" 2>/dev/null || echo 0)"
  PLAN_UNTIL="$(python3 -c "import json; print(json.load(open('$STATE')).get('plan_until_utc','') or '')" 2>/dev/null || true)"
fi
TARGET="${TARGETS[$((IDX % ${#TARGETS[@]}))]}"

log() { echo "[bootstrap-bot $(date -u +%H:%M:%S)] $*" | tee -a "$LOG"; }

# Bound window: if plan_until_utc set and expired → stop placing.
if [[ -n "$PLAN_UNTIL" ]]; then
  if python3 -c "import datetime as d; import sys; now=d.datetime.now(d.timezone.utc); end=d.datetime.fromisoformat('$PLAN_UNTIL'.replace('Z','+00:00')); sys.exit(0 if now<=end else 1)"; then
    :
  else
    log "STOP — plan window ended at $PLAN_UNTIL (no new orders)"
    exit 0
  fi
fi

ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$INSTALL/.env" | cut -d= -f2- | tr -d '\r\n')"
wallet_json="$(curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $ADMIN" "$BASE/api/wallet")"
bal="$(jq -r '.balance_orders_spendable_hmc // .balance_hmc // 0' <<<"$wallet_json")"
bal_total="$(jq -r '.balance_hmc // 0' <<<"$wallet_json")"
log "wallet spendable_hmc=$bal total_hmc=$bal_total target=$TARGET idx=$IDX plan_until=${PLAN_UNTIL:-none}"

MIN_BAL="${MIN_BALANCE_HMC:-8}"
if ! python3 -c "import sys; sys.exit(0 if float('$bal') >= float('$MIN_BAL') else 1)"; then
  log "SKIP order — balance $bal < min $MIN_BAL"
  exit 0
fi

# Light budgets: visible on marketplace, finish in tens of minutes on healthy dig.
RESERVE="${RESERVE_HMC:-100}"
MAX_ORDER="${MAX_ORDER_HMC:-6}"
MIN_PER_RUN="${MIN_PER_RUN_HMC:-0.0001}"
budget="$(python3 -c "b=float('$bal'); r=float('$RESERVE'); m=float('$MAX_ORDER'); print(round(min(m, max(5.0, min(m, b-r))), 4))")"
solves="${TARGET_SOLVES:-4}"
max_runs="$(python3 -c "b=float('$budget'); p=float('$MIN_PER_RUN'); print(int((b*0.20)/p))")"
runs="${BUDGET_RUNS:-384}"
runs="$(python3 -c "r=int('$runs'); m=int('$max_runs'); print(min(max(128,r), m) if m>0 else max(128,r))")"

if [[ "${BOOTSTRAP_DRY_RUN:-0}" == "1" ]]; then
  log "DRY_RUN would place target=$TARGET budget=$budget runs=$runs solves=$solves"
  exit 0
fi

# Snapshot wallet before order (payout tracking).
python3 -c "
import json, time, pathlib
p = pathlib.Path('$PAYOUT_LOG')
row = {
  'ts': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
  'event': 'pre_order',
  'target': '$TARGET',
  'budget_hmc': float('$budget'),
  'budget_runs': int('$runs'),
  'spendable_hmc': float('$bal'),
  'balance_hmc': float('$bal_total'),
}
with p.open('a') as f:
  f.write(json.dumps(row, ensure_ascii=False) + '\n')
"

export BOOTSTRAP_INSTALL="$INSTALL" BUDGET_HMC="$budget" BUDGET_RUNS="$runs" REWARD_HMC="${REWARD_HMC:-0.05}" TARGET_SOLVES="$solves"
# Don't block the timer for hours — campaign keeps running on pool after we exit.
export POLL_SEC="${POLL_SEC:-45}" MAX_WAIT="${MAX_WAIT:-600}"
export HACKME_MINIMAL_POH_GATE="${HACKME_MINIMAL_POH_GATE:-1}"
log "placing target=$TARGET budget_hmc=$budget runs=$runs solves=$solves poh_gate=minimal max_wait=$MAX_WAIT"
set +e
bash "$SCRIPT_DIR/place_bootstrap_order.sh" "$TARGET" >>"$LOG" 2>&1
rc=$?
set -e
log "place_exit=$rc"

# Best-effort: capture last campaign id from order log / state
LAST_CID="$(grep -Eo 'campaign-bootstrap-[a-z0-9]+-[0-9a-z]+' "$LOG" | tail -1 || true)"

wallet_json2="$(curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $ADMIN" "$BASE/api/wallet" || echo '{}')"
bal2="$(jq -r '.balance_orders_spendable_hmc // .balance_hmc // 0' <<<"$wallet_json2")"
bal_total2="$(jq -r '.balance_hmc // 0' <<<"$wallet_json2")"

python3 -c "
import json, pathlib, time
p = pathlib.Path('$STATE')
st = json.loads(p.read_text()) if p.exists() else {}
st['target_idx'] = ($IDX + 1) % ${#TARGETS[@]}
st['last_target'] = '$TARGET'
st['last_budget_hmc'] = float('$budget')
st['last_budget_runs'] = int('$runs')
st['last_run_utc'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
st['last_campaign'] = '$LAST_CID' or st.get('last_campaign')
st['cadence'] = '2_3_per_day_light_3d'
st['last_place_rc'] = int('$rc')
if not st.get('plan_until_utc'):
  # default 3-day window from first armed run
  import datetime as d
  st['plan_until_utc'] = (d.datetime.now(d.timezone.utc) + d.timedelta(days=3)).strftime('%Y-%m-%dT%H:%M:%SZ')
p.write_text(json.dumps(st, indent=2) + '\n')

plog = pathlib.Path('$PAYOUT_LOG')
row = {
  'ts': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
  'event': 'post_order',
  'target': '$TARGET',
  'campaign_id': '$LAST_CID',
  'budget_hmc': float('$budget'),
  'budget_runs': int('$runs'),
  'spendable_hmc': float('$bal2' or 0),
  'balance_hmc': float('$bal_total2' or 0),
  'spendable_delta_hmc': float('$bal2' or 0) - float('$bal'),
  'place_rc': int('$rc'),
}
with plog.open('a') as f:
  f.write(json.dumps(row, ensure_ascii=False) + '\n')
"
log "next target_idx=$(( (IDX + 1) % ${#TARGETS[@]} )) spendable_now=$bal2"
