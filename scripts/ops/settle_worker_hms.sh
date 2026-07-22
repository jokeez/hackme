#!/usr/bin/env bash
# settle_worker_hms.sh — mint HMS accrual from sealed epochs to on-chain HMS balances.
#
# Reads finalized (or finalizes) payouts from hmscoordinator GET /api/seal/payouts,
# then POSTs /api/hms/mint on the chain node.
#
# Safety:
#   DRY_RUN=1 (default) — print mint plan only; never calls mint.
#   DRY_RUN=0            — requires ADMIN_TOKEN and a reachable CHAIN_BASE.
#
# Inputs:
#   HMS_COORD_URL / COORD_URL  — coordinator (default http://127.0.0.1:18082)
#   CHAIN_BASE                 — node (default http://127.0.0.1:18080)
#   ADMIN_TOKEN                — node admin token for /api/hms/mint
#   EPOCH_ID                   — optional single epoch; else all sealed from /api/seal/epochs
#   WORKER_PAYOUT_MAP          — csv worker_id=HMC-addr (HMS credited to same address string)
#   STATE_FILE                 — default data/hms_worker_settlement_state.json
#   MIN_SETTLE_HMS             — skip lines below this (default 0.00000001)
#   EPOCH_LIMIT                — max epochs when scanning (default 50)
set -euo pipefail

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[settle-hms] missing command: $1" >&2
    exit 1
  }
}
require_cmd curl
require_cmd jq
require_cmd python3

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HMS_COORD_URL="${HMS_COORD_URL:-${COORD_URL:-http://127.0.0.1:18082}}"
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
ADMIN_SECRET_FILE="${ADMIN_SECRET_FILE:-${ROOT_DIR}/.secrets/hackme_admin_token}"
DRY_RUN="${DRY_RUN:-1}"
EPOCH_ID="${EPOCH_ID:-}"
WORKER_PAYOUT_MAP="${WORKER_PAYOUT_MAP:-}"
STATE_FILE="${STATE_FILE:-data/hms_worker_settlement_state.json}"
MIN_SETTLE_HMS="${MIN_SETTLE_HMS:-0.00000001}"
EPOCH_LIMIT="${EPOCH_LIMIT:-50}"
CURL_MAX_TIME="${CURL_MAX_TIME:-30}"

if [[ -z "$ADMIN_TOKEN" && -r "$ADMIN_SECRET_FILE" ]]; then
  ADMIN_TOKEN="$(tr -d '\r\n' <"$ADMIN_SECRET_FILE")"
fi

mkdir -p "$(dirname "$STATE_FILE")"
if [[ ! -f "$STATE_FILE" ]]; then
  echo '{"epochs":{},"meta":{"policy":"hms-lane-v2"}}' >"$STATE_FILE"
fi

LOCK_FILE="${STATE_FILE}.flock"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "[settle-hms] skip: another instance holds ${LOCK_FILE}" >&2
  exit 0
fi

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

epochs=()
if [[ -n "$EPOCH_ID" ]]; then
  epochs=("$EPOCH_ID")
else
  if ! epochs_json="$(curl -fsS --max-time "$CURL_MAX_TIME" "${HMS_COORD_URL}/api/seal/epochs?limit=${EPOCH_LIMIT}")"; then
    echo "[settle-hms] ERROR: cannot reach ${HMS_COORD_URL}/api/seal/epochs" >&2
    exit 1
  fi
  while IFS= read -r eid; do
    [[ -n "$eid" && "$eid" != "null" ]] && epochs+=("$eid")
  done < <(echo "$epochs_json" | jq -r '.epochs[]?.epoch_id // empty')
fi

if [[ ${#epochs[@]} -eq 0 ]]; then
  echo "[settle-hms] no sealed epochs"
  exit 0
fi

if [[ "$DRY_RUN" != "1" && -z "$ADMIN_TOKEN" ]]; then
  echo "[settle-hms] ADMIN_TOKEN required when DRY_RUN=0" >&2
  exit 1
fi

if [[ "$DRY_RUN" != "1" ]]; then
  curl -fsS -X POST "${CHAIN_BASE}/api/hms/genesis" \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{}' >/dev/null 2>&1 || \
    echo "[settle-hms] WARN: hms genesis call failed (may already be initialized)" >&2
fi

minted_any=0
for epoch_id in "${epochs[@]}"; do
  settle_json="$(curl -fsS --max-time "$CURL_MAX_TIME" "${HMS_COORD_URL}/api/seal/payouts?epoch_id=${epoch_id}")"
  sealed="$(echo "$settle_json" | jq -r '.settlement.sealed // false')"
  finalized="$(echo "$settle_json" | jq -r '.settlement.payouts_finalized // false')"
  if [[ "$sealed" != "true" ]]; then
    echo "[settle-hms] skip epoch ${epoch_id}: not sealed"
    continue
  fi
  if [[ "$finalized" != "true" ]]; then
    echo "[settle-hms] WARN epoch ${epoch_id}: payouts not finalized after GET" >&2
  fi

  while IFS= read -r row_b64; do
    [[ -z "$row_b64" ]] && continue
    row="$(printf '%s' "$row_b64" | base64 -d)"
    worker_id="$(jq -r '.worker_id' <<<"$row")"
    total_hms="$(jq -r '.total_hms // 0' <<<"$row")"
    total_units="$(jq -r '.total_units // 0' <<<"$row")"
    role="$(jq -r '.role // "seal"' <<<"$row")"
    to_addr="$(jq -r --arg wid "$worker_id" '.[$wid] // ""' <<<"$map_json")"
    if [[ -z "$to_addr" ]]; then
      echo "[settle-hms] skip ${worker_id} epoch=${epoch_id}: no WORKER_PAYOUT_MAP address"
      continue
    fi

    already="$(jq -r --arg e "$epoch_id" --arg w "$worker_id" \
      '.epochs[$e].workers[$w].settled_units // 0' "$STATE_FILE")"
    delta_units="$(python3 - "$total_units" "$already" <<'PY'
import sys
t=int(float(sys.argv[1])); a=int(float(sys.argv[2])); print(max(0, t-a))
PY
)"
    delta_hms="$(python3 - "$delta_units" <<'PY'
import sys
print(f"{int(sys.argv[1])/1e8:.12f}")
PY
)"
    should="$(python3 - "$delta_hms" "$MIN_SETTLE_HMS" <<'PY'
import sys
d=float(sys.argv[1]); m=float(sys.argv[2])
print("1" if d >= m else "0")
PY
)"
    if [[ "$should" != "1" ]]; then
      echo "[settle-hms] skip ${worker_id} epoch=${epoch_id}: delta ${delta_hms} HMS < min"
      continue
    fi

    memo="hms_epoch_settle:${epoch_id}:${worker_id}:${role}"
    if [[ "$DRY_RUN" == "1" ]]; then
      echo "[settle-hms] DRY_RUN would mint ${worker_id} -> ${to_addr} epoch=${epoch_id} delta=${delta_hms} HMS units=${delta_units} role=${role}"
      minted_any=1
      continue
    fi

    if jq -e --arg e "$epoch_id" --arg w "$worker_id" \
      '.epochs[$e].workers[$w].pending_mint != null' "$STATE_FILE" >/dev/null 2>&1; then
      echo "[settle-hms] skip ${worker_id} epoch=${epoch_id}: pending_mint present — not re-minting (CLEAR_PENDING_SETTLE=1)" >&2
      if [[ "${CLEAR_PENDING_SETTLE:-0}" == "1" ]]; then
        tmp="$(mktemp)"
        jq --arg e "$epoch_id" --arg w "$worker_id" \
          'del(.epochs[$e].workers[$w].pending_mint)' "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
      fi
      continue
    fi
    tmp="$(mktemp)"
    jq --arg e "$epoch_id" --arg w "$worker_id" --arg addr "$to_addr" --argjson units "$delta_units" --argjson ts "$(date +%s)" \
      '.epochs[$e].workers[$w].pending_mint = {delta_units:$units,payout_address:$addr,started_unix:$ts}' \
      "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"

    resp="$(curl -sS -X POST "${CHAIN_BASE}/api/hms/mint" \
      -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "$(jq -nc --arg to "$to_addr" --argjson units "$delta_units" --arg memo "$memo" \
        '{to:$to, amount_units: ($units|tonumber), memo:$memo}')")"
    ok="$(jq -r '.ok // false' <<<"$resp")"
    if [[ "$ok" != "true" ]]; then
      echo "[settle-hms] ERROR ${worker_id} epoch=${epoch_id}: $resp" >&2
      tmp="$(mktemp)"
      jq --arg e "$epoch_id" --arg w "$worker_id" \
        'del(.epochs[$e].workers[$w].pending_mint)' "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
      continue
    fi
    new_settled="$(python3 - "$already" "$delta_units" <<'PY'
import sys
print(int(sys.argv[1])+int(sys.argv[2]))
PY
)"
    new_hms="$(python3 - "$new_settled" <<'PY'
import sys
print(f"{int(sys.argv[1])/1e8:.12f}")
PY
)"
    tmp="$(mktemp)"
    jq --arg e "$epoch_id" --arg w "$worker_id" --arg addr "$to_addr" \
      --argjson units "$new_settled" --argjson hms "$new_hms" \
      '.epochs[$e].workers[$w] = {"settled_units": $units, "settled_hms": $hms, "payout_address": $addr}
       | .meta.last_epoch = $e
       | .meta.updated_unix = (now|floor)' \
      "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
    minted_any=1
    echo "[settle-hms] minted ${worker_id} -> ${to_addr} epoch=${epoch_id} delta=${delta_hms} HMS"
  done < <(echo "$settle_json" | jq -r '.settlement.payouts[]? | @base64')
done

if [[ "$minted_any" == "1" ]]; then
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "[settle-hms] DRY_RUN done (no chain writes)"
  else
    echo "[settle-hms] done"
  fi
else
  echo "[settle-hms] nothing to settle"
fi
