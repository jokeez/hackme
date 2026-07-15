#!/usr/bin/env bash
# Bootstrap customer bot — rotates OSS high-yield targets, spends wallet on pool audits.
# Snapshots coordinator distribution each step. No site publish.
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

TARGETS=(nghttp2 md4c cjson)
IDX=0
if [[ -f "$STATE" ]]; then
  IDX="$(python3 -c "import json; print(json.load(open('$STATE')).get('target_idx',0))" 2>/dev/null || echo 0)"
fi
TARGET="${TARGETS[$((IDX % ${#TARGETS[@]}))]}"

log() { echo "[bootstrap-bot $(date -u +%H:%M:%S)] $*" | tee -a "$LOG"; }

ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$INSTALL/.env" | cut -d= -f2- | tr -d '\r\n')"
bal="$(curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $ADMIN" "$BASE/api/wallet" | jq -r '.balance_hmc // 0')"
log "wallet balance_hmc=$bal target=$TARGET idx=$IDX"

MIN_BAL="${MIN_BALANCE_HMC:-8.5}"
if python3 -c "import sys; sys.exit(0 if float('$bal') >= float('$MIN_BAL') else 1)"; then
  :
else
  log "SKIP order — balance $bal < min $MIN_BAL (wait for mining settle or top-up)"
  exit 0
fi

# Scale budget: use up to 12 HMC per order but never below min balance reserve
RESERVE="${RESERVE_HMC:-5}"
budget="$(python3 -c "b=float('$bal'); r=float('$RESERVE'); print(min(12.0, max(8.0, b-r)))")"
runs=384
if python3 -c "import sys; sys.exit(0 if float('$budget') >= 11 else 1)"; then runs=512; fi

if [[ "${BOOTSTRAP_DRY_RUN:-0}" == "1" ]]; then
  log "DRY_RUN would place target=$TARGET budget=$budget runs=$runs"
  exit 0
fi

export BOOTSTRAP_INSTALL="$INSTALL" BUDGET_HMC="$budget" BUDGET_RUNS="$runs"
bash "$SCRIPT_DIR/place_bootstrap_order.sh" "$TARGET" >>"$LOG" 2>&1

python3 -c "
import json, pathlib, time
p = pathlib.Path('$STATE')
st = json.loads(p.read_text()) if p.exists() else {}
st['target_idx'] = ($IDX + 1) % ${#TARGETS[@]}
st['last_target'] = '$TARGET'
st['last_run_utc'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
p.write_text(json.dumps(st, indent=2) + '\n')
"
log "next target_idx=$(( (IDX + 1) % ${#TARGETS[@]} ))"
