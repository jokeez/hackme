#!/usr/bin/env bash
# settle_worker_sup.sh — mint coordinator SUP accrual to on-chain SUP balances (admin / treasury emission).
set -euo pipefail

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[settle-sup] missing command: $1" >&2
    exit 1
  }
}
require_cmd curl
require_cmd jq
require_cmd python3

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-${HACKME_COORDINATOR_ADMIN_TOKEN:-${COORD_TOKEN:-}}}"
MIN_SETTLE_SUP="${MIN_SETTLE_SUP:-0.01}"
STATE_FILE="${STATE_FILE:-data/worker_settlement_state.json}"
WORKER_PAYOUT_MAP="${WORKER_PAYOUT_MAP:-}"
ADMIN_SECRET_FILE="${ADMIN_SECRET_FILE:-${ROOT_DIR}/.secrets/hackme_admin_token}"
COORD_SECRET_FILE="${COORD_SECRET_FILE:-${ROOT_DIR}/.secrets/hackme_coordinator_admin_token}"

if [[ -z "$ADMIN_TOKEN" && -r "$ADMIN_SECRET_FILE" ]]; then
  ADMIN_TOKEN="$(tr -d '\r\n' <"$ADMIN_SECRET_FILE")"
fi
if [[ -z "$COORD_ADMIN_TOKEN" && -r "$COORD_SECRET_FILE" ]]; then
  COORD_ADMIN_TOKEN="$(tr -d '\r\n' <"$COORD_SECRET_FILE")"
fi
if [[ -z "$ADMIN_TOKEN" || -z "$COORD_ADMIN_TOKEN" ]]; then
  echo "[settle-sup] ADMIN_TOKEN and COORD_ADMIN_TOKEN required" >&2
  exit 1
fi

# Ensure SUP genesis only when mint is not live yet.
# (Genesis is now idempotent and will not wipe total_minted; still avoid
# hammering it every settle tick.)
sup_live="$(curl -fsS --max-time 8 "${CHAIN_BASE}/api/sup/economics" 2>/dev/null | jq -r '.economics.mint_enabled // false' 2>/dev/null || echo false)"
if [[ "$sup_live" != "true" ]]; then
  curl -fsS -X POST "${CHAIN_BASE}/api/sup/genesis" \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{}' >/dev/null 2>&1 || {
    echo "[settle-sup] WARN: sup genesis call failed (chain may already be initialized)" >&2
  }
fi

mkdir -p "$(dirname "$STATE_FILE")"
[[ -f "$STATE_FILE" ]] || echo '{"workers":{},"meta":{}}' >"$STATE_FILE"

# Share the HMC settler flock: both scripts RMW the same STATE_FILE (H1).
LOCK_FILE="${STATE_FILE}.flock"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "[settle-sup] skip: another instance holds ${LOCK_FILE}"
  exit 0
fi

stats_json="$(curl -fsS -H "X-Hackme-Admin-Token: ${COORD_ADMIN_TOKEN}" "${COORD_URL}/api/work/stats?details=1")"
workers_json="$(echo "$stats_json" | jq -c '.workers // {}')"

map_json='{}'
if [[ -n "$WORKER_PAYOUT_MAP" ]]; then
  map_json="$(python3 - "$WORKER_PAYOUT_MAP" <<'PY'
import json,sys
out={}
for part in sys.argv[1].split(','):
    p=part.strip()
    if '=' in p:
        k,v=p.split('=',1)
        out[k.strip()]=v.strip()
print(json.dumps(out))
PY
)"
fi

settled_any=0
while IFS= read -r item; do
  [[ -z "$item" ]] && continue
  row="$(printf '%s' "$item" | base64 -d)"
  worker_id="$(jq -r '.key' <<<"$row")"
  payout_sup="$(jq -r '.value.payout_sup // 0' <<<"$row")"
  signed_addr="$(jq -r '.value.payout_address // ""' <<<"$row")"
  map_addr="$(jq -r --arg wid "$worker_id" '.[$wid] // ""' <<<"$map_json")"
  to_addr="${map_addr:-$signed_addr}"
  if [[ -z "$to_addr" ]]; then
    echo "[settle-sup] skip ${worker_id}: no payout address"
    continue
  fi
  already_sup="$(jq -r --arg wid "$worker_id" '.workers[$wid].settled_sup // 0' "$STATE_FILE")"
  delta_sup="$(python3 - "$payout_sup" "$already_sup" <<'PY'
import sys
p=float(sys.argv[1]); s=float(sys.argv[2]); d=p-s
print(f"{max(0.0,d):.12f}")
PY
)"
  should="$(python3 - "$delta_sup" "$MIN_SETTLE_SUP" <<'PY'
import sys
d=float(sys.argv[1]); m=float(sys.argv[2])
print("1" if d >= m else "0")
PY
)"
  if [[ "$should" != "1" ]]; then
    echo "[settle-sup] skip ${worker_id}: delta ${delta_sup} SUP < min ${MIN_SETTLE_SUP}"
    continue
  fi
  if jq -e --arg wid "$worker_id" '.workers[$wid].pending_sup != null' "$STATE_FILE" >/dev/null 2>&1; then
    echo "[settle-sup] skip ${worker_id}: pending_sup present — not re-minting" >&2
    echo "[settle-sup] tip: PROMOTE_PENDING_SUP=1 records pending as settled only if mint already succeeded; CLEAR_PENDING_SUP=1 drops pending to retry mint" >&2
    if [[ "${PROMOTE_PENDING_SUP:-0}" == "1" ]]; then
      # Prefer PROMOTE_PENDING_SUP. CLEAR_PENDING_SETTLE no longer aliases to promote (HMC drop semantics).
      pending_amt="$(jq -r --arg wid "$worker_id" '.workers[$wid].pending_sup.delta_sup // 0' "$STATE_FILE")"
      pending_addr="$(jq -r --arg wid "$worker_id" '.workers[$wid].pending_sup.payout_address // ""' "$STATE_FILE")"
      already_now="$(jq -r --arg wid "$worker_id" '.workers[$wid].settled_sup // 0' "$STATE_FILE")"
      new_settled="$(python3 - "$already_now" "$pending_amt" <<'PY'
import sys
print(f"{float(sys.argv[1])+float(sys.argv[2]):.12f}")
PY
)"
      tmp="$(mktemp)"
      jq --arg wid "$worker_id" --arg addr "$pending_addr" --argjson settled "$new_settled" \
        '.workers[$wid].settled_sup = $settled
         | (if ($addr|length)>0 then .workers[$wid].payout_address = $addr else . end)
         | del(.workers[$wid].pending_sup)' \
        "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
      echo "[settle-sup] PROMOTE_PENDING_SUP promoted ${worker_id} settled_sup=${new_settled}" >&2
    elif [[ "${CLEAR_PENDING_SUP:-0}" == "1" ]]; then
      tmp="$(mktemp)"
      jq --arg wid "$worker_id" 'del(.workers[$wid].pending_sup)' "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
      echo "[settle-sup] CLEAR_PENDING_SUP dropped pending for ${worker_id} (will retry mint)" >&2
    fi
    continue
  fi
  tmp="$(mktemp)"
  jq --arg wid "$worker_id" --arg addr "$to_addr" --argjson amt "$delta_sup" --argjson ts "$(date +%s)" \
    '.workers[$wid].pending_sup = {delta_sup:$amt,payout_address:$addr,started_unix:$ts}' \
    "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
  resp=""
  ok="false"
  # Memo includes settled cursor so a larger retry delta cannot reuse an old idem key (M-CRIT-02).
  memo="worker_sup_settlement:${worker_id}:settled=${already_sup}:delta=${delta_sup}"
  for attempt in 1 2 3 4 5; do
    resp="$(curl -sS -X POST "${CHAIN_BASE}/api/sup/mint" \
      -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "$(jq -nc --arg to "$to_addr" --argjson amt "$delta_sup" --arg memo "$memo" \
        '{to:$to, amount_sup: ($amt|tonumber), memo:$memo}')")"
    ok="$(jq -r '.ok // false' <<<"$resp")"
    if [[ "$ok" == "true" ]]; then
      break
    fi
    if [[ "$resp" == *"database is locked"* || "$resp" == *"SQLITE_BUSY"* ]]; then
      sleep $((attempt * 2))
      continue
    fi
    break
  done
  if [[ "$ok" != "true" ]]; then
    echo "[settle-sup] ERROR ${worker_id}: $resp" >&2
    tmp="$(mktemp)"
    jq --arg wid "$worker_id" 'del(.workers[$wid].pending_sup)' "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
    continue
  fi
  new_settled="$(python3 - "$already_sup" "$delta_sup" <<'PY'
import sys
print(f"{float(sys.argv[1])+float(sys.argv[2]):.12f}")
PY
)"
  tmp="$(mktemp)"
  jq --arg wid "$worker_id" --arg addr "$to_addr" --argjson settled "$new_settled" \
    '.workers[$wid].settled_sup = $settled | .workers[$wid].payout_address = $addr | del(.workers[$wid].pending_sup)' \
    "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
  settled_any=1
  echo "[settle-sup] minted ${worker_id} -> ${to_addr} delta=${delta_sup} SUP"
done < <(jq -r 'to_entries[] | @base64' <<<"$workers_json")

if [[ "$settled_any" == "1" ]]; then
  echo "[settle-sup] done"
else
  echo "[settle-sup] nothing to settle"
fi
