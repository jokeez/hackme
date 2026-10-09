#!/usr/bin/env bash
# Bootstrap Hunt bot — places 1 Hunt campaign per tick alongside Dig bot.
# Cadence: same timer as Dig (06/14/22 UTC) via hackme-bootstrap-hunt.timer
set -euo pipefail
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE="${BASE:-http://127.0.0.1:8080}"
LOG="$INSTALL/logs/bootstrap/hunt_bot.log"
STATE="$INSTALL/logs/bootstrap/hunt_bot_state.json"
DIG_STATE="$INSTALL/logs/bootstrap/bot_state.json"
mkdir -p "$INSTALL/logs/bootstrap" "$(dirname "$STATE")"

# Rotate light Hunt targets (ASAN rail).
TARGETS=(jsmn cjson md4c yyjson nghttp2 expat)
PKGS=(hunt_standard hunt_standard hunt_lite hunt_standard hunt_lite hunt_lite)
SHARDS=(32 32 24 32 24 24)
IDX=0
if [[ -f "$STATE" ]]; then
  IDX="$(python3 -c "import json; print(json.load(open('$STATE')).get('target_idx',0))" 2>/dev/null || echo 0)"
fi
# Honor Dig plan window if set.
PLAN_UNTIL=""
if [[ -f "$DIG_STATE" ]]; then
  PLAN_UNTIL="$(python3 -c "import json; print(json.load(open('$DIG_STATE')).get('plan_until_utc','') or '')" 2>/dev/null || true)"
fi

log() { echo "[hunt-bot $(date -u +%H:%M:%S)] $*" | tee -a "$LOG"; }

# Same autorenew as Dig bot so hunt timer does not stall after plan expiry.
AUTORENEW_DAYS="${BOOTSTRAP_PLAN_AUTORENEW_DAYS:-14}"
if [[ -n "$PLAN_UNTIL" ]]; then
  if ! python3 -c "import datetime as d,sys; now=d.datetime.now(d.timezone.utc); end=d.datetime.fromisoformat('$PLAN_UNTIL'.replace('Z','+00:00')); sys.exit(0 if now<=end else 1)"; then
    if [[ "$AUTORENEW_DAYS" =~ ^[0-9]+$ ]] && (( AUTORENEW_DAYS > 0 )); then
      NEW_UNTIL="$(python3 -c "import datetime as d; print((d.datetime.now(d.timezone.utc)+d.timedelta(days=int('$AUTORENEW_DAYS'))).strftime('%Y-%m-%dT%H:%M:%SZ'))")"
      python3 -c "
import json, pathlib, time
p = pathlib.Path('$DIG_STATE')
st = json.loads(p.read_text()) if p.exists() else {}
st['plan_until_utc'] = '$NEW_UNTIL'
st['plan_extended_utc'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
st['plan_extended_days'] = int('$AUTORENEW_DAYS')
st['plan_autorenewed_from'] = '$PLAN_UNTIL'
p.write_text(json.dumps(st, indent=2) + '\n')
"
      PLAN_UNTIL="$NEW_UNTIL"
      log "AUTORENEW dig plan_until $PLAN_UNTIL (was expired; +${AUTORENEW_DAYS}d)"
    else
      log "STOP — Dig plan window ended at $PLAN_UNTIL"
      exit 0
    fi
  fi
fi

ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$INSTALL/.env" | cut -d= -f2- | tr -d '\r\n')"
wallet_json="$(curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $ADMIN" "$BASE/api/wallet")"
bal="$(jq -r '.balance_orders_spendable_hmc // .balance_hmc // 0' <<<"$wallet_json")"
MIN_BAL="${MIN_BALANCE_HMC:-8}"
if ! python3 -c "import sys; sys.exit(0 if float('$bal') >= float('$MIN_BAL') else 1)"; then
  log "SKIP — spendable $bal < $MIN_BAL"
  exit 0
fi

i=$((IDX % ${#TARGETS[@]}))
TARGET="${TARGETS[$i]}"
PKG="${PKGS[$i]}"
N="${SHARDS[$i]}"

if [[ "${BOOTSTRAP_DRY_RUN:-0}" == "1" ]]; then
  log "DRY_RUN would place target=$TARGET pkg=$PKG shards=$N"
  exit 0
fi

log "placing target=$TARGET pkg=$PKG shards=$N spendable=$bal plan_until=${PLAN_UNTIL:-none}"
set +e
bash "$SCRIPT_DIR/place_bootstrap_hunt.sh" "$TARGET" "$PKG" "$N" >>"$LOG" 2>&1
rc=$?
set -e
log "place_exit=$rc"

python3 -c "
import json, pathlib, time
p = pathlib.Path('$STATE')
st = json.loads(p.read_text()) if p.exists() else {}
st['target_idx'] = ($IDX + 1) % ${#TARGETS[@]}
st['last_target'] = '$TARGET'
st['last_pkg'] = '$PKG'
st['last_shards'] = int('$N')
st['last_run_utc'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
st['last_place_rc'] = int('$rc')
p.write_text(json.dumps(st, indent=2) + '\n')
"
log "next target_idx=$(( (IDX + 1) % ${#TARGETS[@]} ))"
