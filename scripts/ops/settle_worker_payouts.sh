#!/usr/bin/env bash
set -euo pipefail

# Settle coordinator off-chain worker accruals to on-chain transfers.
#
# Inputs:
# - COORD_URL: coordinator base (default http://127.0.0.1:18081)
# - CHAIN_BASE: canonical node base (default http://127.0.0.1:18080)
# - ADMIN_TOKEN: node admin token used for /api/tx/send
# - COORD_ADMIN_TOKEN / HACKME_COORDINATOR_ADMIN_TOKEN: coordinator admin (required for ?details=1 after hardening)
# - MIN_SETTLE_HMC: minimum delta per worker to settle (default 0.01)
# - DAILY_FORCE_INTERVAL_SEC: force-settle cycle cadence (default 86400 = once/day)
# - DAILY_MIN_SETTLE_HMC: minimal delta used during force-settle cycle (default 0.0001)
# - FORCE_SETTLE_ALL: set 1 to force daily branch immediately for this run
# - STATE_FILE: local settlement state file (default data/worker_settlement_state.json)
# - WORKER_PAYOUT_MAP: optional csv "worker_id=HMC-...,worker2=HMC-..."
#
# Notes:
# - Primary payout address source: workers[*].payout_address from hybrid signed submits.
# - Fallback source: WORKER_PAYOUT_MAP mapping.

require_cmd() {
  # VPS safety: broken /dev/null (regular file) breaks `command -v … >/dev/null`.
  if command -v "$1" 2>&1 | grep -q .; then
    return 0
  fi
  echo "[settle-workers] missing command: $1 (PATH=${PATH:-})" >&2
  exit 1
}

require_cmd curl
require_cmd jq
require_cmd python3

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_SECRET_FILE="${COORD_SECRET_FILE:-${ROOT_DIR}/.secrets/hackme_coordinator_admin_token}"
ADMIN_SECRET_FILE="${ADMIN_SECRET_FILE:-${ROOT_DIR}/.secrets/hackme_admin_token}"

COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-${HACKME_COORDINATOR_ADMIN_TOKEN:-${COORD_TOKEN:-}}}"
MIN_SETTLE_HMC="${MIN_SETTLE_HMC:-0.0001}"
DAILY_FORCE_INTERVAL_SEC="${DAILY_FORCE_INTERVAL_SEC:-86400}"
DAILY_MIN_SETTLE_HMC="${DAILY_MIN_SETTLE_HMC:-0.0001}"
FORCE_SETTLE_ALL="${FORCE_SETTLE_ALL:-0}"
STATE_FILE="${STATE_FILE:-data/worker_settlement_state.json}"
WORKER_PAYOUT_MAP="${WORKER_PAYOUT_MAP:-}"
SETTLE_TX_WAIT_SEC="${SETTLE_TX_WAIT_SEC:-90}"
SETTLE_SEQUENTIAL="${SETTLE_SEQUENTIAL:-1}"
SETTLE_PAYOUT_PAUSE_SEC="${SETTLE_PAYOUT_PAUSE_SEC:-10}"
SETTLE_NONCE_RETRIES="${SETTLE_NONCE_RETRIES:-6}"
MIN_FEE_UNITS=1000
UNITS_PER_HMC=100000000

if [[ -z "$ADMIN_TOKEN" && -r "$ADMIN_SECRET_FILE" ]]; then
  ADMIN_TOKEN="$(tr -d '\r\n' <"$ADMIN_SECRET_FILE")"
fi
if [[ -z "$COORD_ADMIN_TOKEN" && -r "$COORD_SECRET_FILE" ]]; then
  COORD_ADMIN_TOKEN="$(tr -d '\r\n' <"$COORD_SECRET_FILE")"
fi
if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[settle-workers] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN or ${ADMIN_SECRET_FILE}) is required" >&2
  exit 1
fi
if [[ -z "$COORD_ADMIN_TOKEN" ]]; then
  echo "[settle-workers] COORD_ADMIN_TOKEN required for coordinator /api/work/stats?details=1 (set env or ${COORD_SECRET_FILE})" >&2
  exit 1
fi

mkdir -p "$(dirname "$STATE_FILE")"
if [[ ! -f "$STATE_FILE" ]]; then
  echo '{"workers":{},"meta":{"last_force_unix":0}}' >"$STATE_FILE"
fi
# systemd runs as User=hackme; state must not be root-only.
if [[ "$(id -u)" -eq 0 ]]; then
  chown hackme:hackme "$(dirname "$STATE_FILE")" "$STATE_FILE" 2>/dev/null || true
  chmod 700 "$(dirname "$STATE_FILE")" 2>/dev/null || true
  chmod 600 "$STATE_FILE" 2>/dev/null || true
fi
if [[ ! -r "$STATE_FILE" || ! -w "$STATE_FILE" ]]; then
  echo "[settle-workers] cannot read/write STATE_FILE=${STATE_FILE} (run: sudo chown hackme:hackme ${STATE_FILE})" >&2
  exit 1
fi

# Single-writer: avoid two cron/systemd instances racing on nonce + state (see docs/POOL_SECURITY_THREATS_VERDICT.md).
LOCK_FILE="${STATE_FILE}.flock"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "[settle-workers] skip: another instance holds ${LOCK_FILE}" >&2
  exit 0
fi

tx_pool_pending_count() {
  curl -fsS "${CHAIN_BASE}/api/tx/pool" | jq '.txs | if . == null then 0 else length end' 2>/dev/null || echo 0
}

wait_tx_pool_clear() {
  local deadline=$((SECONDS + SETTLE_TX_WAIT_SEC))
  while (( SECONDS < deadline )); do
    if [[ "$(tx_pool_pending_count)" == "0" ]]; then
      return 0
    fi
    sleep 5
  done
  echo "[settle-workers] warn: tx pool still pending after ${SETTLE_TX_WAIT_SEC}s" >&2
  return 1
}

send_settlement_tx() {
  local from_addr="$1" to_addr="$2" amount_units="$3" ts="$4" nonce="$5"
  local tx_body resp
  tx_body="$(jq -nc \
    --arg from "$from_addr" \
    --arg to "$to_addr" \
    --argjson amount "$amount_units" \
    --argjson fee "$MIN_FEE_UNITS" \
    --argjson nonce "$nonce" \
    --argjson ts "$ts" \
    '{tx_type:"transfer_v1",from:$from,to:$to,amount_units:$amount,fee_units:$fee,nonce:$nonce,timestamp_unix:$ts,memo:"worker_settlement"}')"
  resp="$(curl -sS -X POST "${CHAIN_BASE}/api/tx/send" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
    -d "$tx_body")"
  printf '%s' "$resp"
}

if ! jq -e '.workers | type=="object"' "$STATE_FILE" >/dev/null 2>&1; then
  tmp="$(mktemp)"
  jq -c '{workers:(.workers // {}),meta:(.meta // {})}' "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
fi

now_unix="$(date +%s)"
force_settle="$FORCE_SETTLE_ALL"
if [[ "$force_settle" != "1" ]]; then
  last_force_unix="$(jq -r '.meta.last_force_unix // 0' "$STATE_FILE" 2>/dev/null || echo 0)"
  if ! [[ "$last_force_unix" =~ ^[0-9]+$ ]]; then
    last_force_unix=0
  fi
  elapsed="$((now_unix - last_force_unix))"
  if [[ "$elapsed" -ge "$DAILY_FORCE_INTERVAL_SEC" ]]; then
    force_settle=1
  fi
fi
if [[ "$force_settle" == "1" ]]; then
  echo "[settle-workers] force-settle mode ON (interval=${DAILY_FORCE_INTERVAL_SEC}s daily_min=${DAILY_MIN_SETTLE_HMC} HMC)"
fi

stats_json="$(curl -fsS -H "X-Hackme-Admin-Token: ${COORD_ADMIN_TOKEN}" "${COORD_URL}/api/work/stats?details=1")"
payer_addr="$(curl -fsS -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" "${CHAIN_BASE}/api/wallet" | jq -r '.address // ""')"
if [[ -z "$payer_addr" ]]; then
  echo "[settle-workers] failed to resolve payer wallet address from ${CHAIN_BASE}/api/wallet" >&2
  exit 1
fi

map_json='{}'
if [[ -n "$WORKER_PAYOUT_MAP" ]]; then
  map_json="$(python3 - "$WORKER_PAYOUT_MAP" <<'PY'
import json,sys
raw=sys.argv[1].strip()
out={}
for part in raw.split(','):
    p=part.strip()
    if not p or '=' not in p:
        continue
    k,v=p.split('=',1)
    k=k.strip(); v=v.strip()
    if k and v:
        out[k]=v
print(json.dumps(out))
PY
)"
fi

# Public coordinators often omit workers{} even with ?details=1; without rows the script used to exit and never pay.
workers_json="$(echo "$stats_json" | jq -c '.workers // {} | if type == "object" then . else {} end')"
if [[ "$workers_json" == "{}" ]]; then
  wcount="$(echo "$stats_json" | jq '(.workers_count // 0) | floor')"
  tp="$(echo "$stats_json" | jq -r '.total_payout_hmc // 0')"
  nmap="$(jq 'length' <<<"$map_json")"
  tp_pos="$(echo "$stats_json" | jq '((.total_payout_hmc // 0) | tonumber) > 0')"
  if [[ "$wcount" -gt 0 ]] && [[ "$tp_pos" == "true" ]]; then
    if [[ "$nmap" -eq 1 ]]; then
      wid="$(jq -r 'keys[0]' <<<"$map_json")"
      workers_json="$(echo "$stats_json" | jq -c --arg wid "$wid" '{($wid): {payout_hmc: .total_payout_hmc}}')"
      echo "[settle-workers] coordinator omitted workers{} — synthetic row for ${wid} using pool total_payout_hmc=${tp} HMC (single WORKER_PAYOUT_MAP entry)" >&2
    elif [[ "$nmap" -gt 1 ]]; then
      echo "[settle-workers] ERROR: workers{} empty but WORKER_PAYOUT_MAP has ${nmap} keys; cannot split pool total safely. Set one worker id, upgrade coordinator to expose workers{}, or run separate pools." >&2
      exit 2
    else
      echo "[settle-workers] no workers in coordinator stats — set WORKER_PAYOUT_MAP=worker-id=HMC-... (one entry) to settle when API omits workers{}" >&2
      exit 0
    fi
  else
    echo "[settle-workers] no workers in coordinator stats"
    exit 0
  fi
fi

workers_list="$(jq -r 'to_entries[] | @base64' <<<"$workers_json")"
if [[ -z "$workers_list" ]]; then
  echo "[settle-workers] no worker entries"
  exit 0
fi

settled_any=0
payouts_sent=0
while IFS= read -r item; do
  [[ -z "$item" ]] && continue
  row="$(printf '%s' "$item" | base64 -d)"
  worker_id="$(jq -r '.key' <<<"$row")"
  payout_hmc="$(jq -r '.value.payout_hmc // 0' <<<"$row")"
  signed_addr="$(jq -r '.value.payout_address // ""' <<<"$row")"
  map_addr="$(jq -r --arg wid "$worker_id" '.[$wid] // ""' <<<"$map_json")"
  # WORKER_PAYOUT_MAP overrides hybrid signed address when set (operator payout routing).
  if [[ -n "$map_addr" ]]; then
    to_addr="$map_addr"
  else
    to_addr="$signed_addr"
  fi
  if [[ -z "$to_addr" ]]; then
    echo "[settle-workers] skip ${worker_id}: no payout address (signed/map)"
    continue
  fi
  if [[ "$to_addr" == "$payer_addr" ]]; then
    echo "[settle-workers] skip ${worker_id}: payout address equals payer address (${payer_addr})"
    continue
  fi

  already_hmc="$(jq -r --arg wid "$worker_id" '.workers[$wid].settled_hmc // 0' "$STATE_FILE")"
  delta_hmc="$(python3 - "$payout_hmc" "$already_hmc" <<'PY'
import sys
p=float(sys.argv[1]); s=float(sys.argv[2]); d=p-s
if d < 0: d = 0.0
print(f"{d:.12f}")
PY
)"
  should_settle="$(python3 - "$delta_hmc" "$MIN_SETTLE_HMC" "$force_settle" "$DAILY_MIN_SETTLE_HMC" <<'PY'
import sys
d=float(sys.argv[1]); m=float(sys.argv[2]); force=(sys.argv[3]=="1"); dm=float(sys.argv[4])
ok = (d >= m) or (force and d >= dm)
print("1" if ok else "0")
PY
)"
  if [[ "$should_settle" != "1" ]]; then
    if [[ "$force_settle" == "1" ]]; then
      echo "[settle-workers] skip ${worker_id}: delta ${delta_hmc} HMC < force-daily min ${DAILY_MIN_SETTLE_HMC}"
    else
      echo "[settle-workers] skip ${worker_id}: delta ${delta_hmc} HMC < min ${MIN_SETTLE_HMC}"
    fi
    continue
  fi

  amount_units="$(python3 - "$delta_hmc" <<'PY'
import sys
u=int(float(sys.argv[1])*100000000+0.5)
print(max(0,u))
PY
)"
  if [[ "$amount_units" -le 0 ]]; then
    echo "[settle-workers] skip ${worker_id}: computed amount_units=0"
    continue
  fi

  ts="$(date +%s)"
  if [[ "$SETTLE_SEQUENTIAL" == "1" && "$payouts_sent" -gt 0 ]]; then
    wait_tx_pool_clear || true
    if [[ "$SETTLE_PAYOUT_PAUSE_SEC" -gt 0 ]]; then
      sleep "$SETTLE_PAYOUT_PAUSE_SEC"
    fi
  fi
  nonce="$(curl -fsS "${CHAIN_BASE}/api/address/${payer_addr}" | jq -r '.next_nonce // 0')"

  resp="$(send_settlement_tx "$payer_addr" "$to_addr" "$amount_units" "$ts" "$nonce")"
  ok="$(jq -r '.ok // false' <<<"$resp" 2>/dev/null || echo "false")"
  code="$(jq -r '.code // ""' <<<"$resp" 2>/dev/null || echo "")"
  attempt=1
  while [[ "$ok" != "true" && "$code" == "pending_nonce_conflict" && "$attempt" -lt "$SETTLE_NONCE_RETRIES" ]]; do
    wait_tx_pool_clear || true
    sleep $((SETTLE_PAYOUT_PAUSE_SEC + attempt * 2))
    nonce="$(curl -fsS "${CHAIN_BASE}/api/address/${payer_addr}" | jq -r '.next_nonce // 0')"
    resp="$(send_settlement_tx "$payer_addr" "$to_addr" "$amount_units" "$ts" "$nonce")"
    ok="$(jq -r '.ok // false' <<<"$resp" 2>/dev/null || echo "false")"
    code="$(jq -r '.code // ""' <<<"$resp" 2>/dev/null || echo "")"
    attempt=$((attempt + 1))
  done
  if [[ "$ok" != "true" ]]; then
    echo "[settle-workers] ERROR settle ${worker_id} -> ${to_addr}: ${resp}" >&2
    continue
  fi
  tx_hash="$(jq -r '.tx_hash // ""' <<<"$resp")"
  settled_any=1
  payouts_sent=$((payouts_sent + 1))
  echo "[settle-workers] settled ${worker_id} -> ${to_addr} delta=${delta_hmc} HMC tx=${tx_hash}"
  new_settled_hmc="$(python3 - "$already_hmc" "$delta_hmc" <<'PY'
import sys
already=float(sys.argv[1]); delta=float(sys.argv[2])
print(f"{already + delta:.12f}")
PY
)"
  tmp="$(mktemp)"
  jq --arg wid "$worker_id" --arg addr "$to_addr" --argjson settled "$new_settled_hmc" --arg tx "$tx_hash" --argjson ts "$ts" \
    '.workers[$wid] = {settled_hmc:$settled,payout_address:$addr,last_tx_hash:$tx,last_settle_unix:$ts}' \
    "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
  if [[ "$(id -u)" -eq 0 ]]; then
    chown hackme:hackme "$STATE_FILE" 2>/dev/null || true
    chmod 600 "$STATE_FILE" 2>/dev/null || true
  fi
done <<<"$workers_list"

if [[ "$settled_any" == "1" ]]; then
  echo "[settle-workers] done: payouts submitted"
else
  echo "[settle-workers] done: nothing to settle"
fi
if [[ "$force_settle" == "1" ]]; then
  tmp="$(mktemp)"
  jq --argjson ts "$now_unix" '.meta.last_force_unix = $ts' "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
fi
