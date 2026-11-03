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
STATE_DB="${STATE_DB:-}"
if [[ -z "$STATE_DB" && "${SETTLE_SQLITE:-1}" != "0" ]]; then
  if [[ "$STATE_FILE" == *.json ]]; then
    STATE_DB="${STATE_FILE%.json}.db"
  else
    STATE_DB="${STATE_FILE}.db"
  fi
fi
SETTLECTL="${SETTLECTL:-}"
if [[ -z "$SETTLECTL" ]]; then
  for c in "$ROOT_DIR/bin/workersettlectl" "$ROOT_DIR/workersettlectl"; do
    if [[ -x "$c" ]]; then
      SETTLECTL="$c"
      break
    fi
  done
fi
CURL_MAX_TIME="${CURL_MAX_TIME:-60}"
WORKER_PAYOUT_MAP="${WORKER_PAYOUT_MAP:-}"
ADMIN_SECRET_FILE="${ADMIN_SECRET_FILE:-${ROOT_DIR}/.secrets/hackme_admin_token}"
COORD_SECRET_FILE="${COORD_SECRET_FILE:-${ROOT_DIR}/.secrets/hackme_coordinator_admin_token}"

settle_sqlite_ready() {
  [[ -n "$STATE_DB" && "${SETTLE_SQLITE:-1}" != "0" && -n "$SETTLECTL" && -x "$SETTLECTL" ]]
}

# Prefer SQLite settled_sup when present (never lose to HMC export rollback).
settle_read_settled_sup() {
  local wid="$1"
  if settle_sqlite_ready; then
    local row
    if row="$("$SETTLECTL" get-json "$STATE_DB" "$wid" 2>/dev/null)"; then
      jq -r '.settled_sup // 0' <<<"$row"
      return 0
    fi
  fi
  jq -r --arg wid "$wid" '.workers[$wid].settled_sup // 0' "$STATE_FILE"
}

settle_read_session_anchor_sup() {
  local wid="$1"
  if settle_sqlite_ready; then
    local row a
    if row="$("$SETTLECTL" get-json "$STATE_DB" "$wid" 2>/dev/null)"; then
      a="$(jq -r '.session_anchor_sup // empty' <<<"$row")"
      if [[ -n "$a" && "$a" != "0" && "$a" != "0.0" ]]; then
        echo "$a"
        return 0
      fi
    fi
  fi
  jq -r --arg wid "$wid" '.workers[$wid].session_anchor_sup // empty' "$STATE_FILE"
}

settle_write_settled_sup() {
  local wid="$1" settled="$2" addr="$3"
  if settle_sqlite_ready; then
    if ! "$SETTLECTL" set-sup "$STATE_DB" "$wid" "$settled" "$addr" >/dev/null; then
      echo "[settle-sup] FATAL: set-sup failed for ${wid}" >&2
      return 1
    fi
  fi
  return 0
}

settle_write_session_anchor_sup() {
  local wid="$1" anchor="$2"
  if settle_sqlite_ready; then
    if [[ -z "$anchor" || "$anchor" == "clear" ]]; then
      "$SETTLECTL" set-sup-anchor "$STATE_DB" "$wid" clear >/dev/null || true
    else
      "$SETTLECTL" set-sup-anchor "$STATE_DB" "$wid" "$anchor" >/dev/null || true
    fi
  fi
}

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

# Economics v2: SUP is emitted only by blocks to the settlement wallet and admin mint is
# retired on chain. Settlement must then pay SUP by transfer (Go settler, nonce-safe) — never
# fall back to genesis/mint here. Accrual keeps growing in the coordinator until that runs.
sup_econ="$(curl -fsS --max-time 8 "${CHAIN_BASE}/api/sup/economics" 2>/dev/null || true)"
if [[ "$(jq -r '.economics.admin_mint_retired // false' <<<"$sup_econ" 2>/dev/null)" == "true" ]]; then
  echo "[settle-sup] economics v2: admin SUP mint retired — mint settlement disabled (pay by transfer)" >&2
  exit 0
fi

# Ensure SUP genesis only when mint is not live yet.
# (Genesis is now idempotent and will not wipe total_minted; still avoid
# hammering it every settle tick.)
sup_live="$(jq -r '.economics.mint_enabled // false' <<<"$sup_econ" 2>/dev/null || echo false)"
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
  if [[ "$(jq -r '.value.address_conflict // false' <<<"$row")" == "true" ]]; then
    echo "[settle-sup] skip ${worker_id}: address_conflict — refuse payout (fleet merge)" >&2
    continue
  fi
  payout_sup="$(jq -r '.value.payout_sup // 0' <<<"$row")"
  signed_addr="$(jq -r '.value.payout_address // ""' <<<"$row")"
  map_addr="$(jq -r --arg wid "$worker_id" '.[$wid] // ""' <<<"$map_json")"
  to_addr="${map_addr:-$signed_addr}"
  if [[ -z "$to_addr" ]]; then
    echo "[settle-sup] skip ${worker_id}: no payout address"
    continue
  fi
  already_sup="$(settle_read_settled_sup "$worker_id")"
  # Mirror HMC session-anchor: coordinator payout_sup is in-memory and resets on restart.
  # Never lower settled_sup; pay only session growth above the anchor until accrual catches up.
  session_anchor="$(settle_read_session_anchor_sup "$worker_id")"
  rebase="$(python3 - "$payout_sup" "$already_sup" <<'PY'
import sys
p=float(sys.argv[1]); s=float(sys.argv[2])
print("1" if (s - p) > 1e-9 else "0")
PY
)"
  if [[ "$rebase" == "1" ]]; then
    # Same second-reset trap as HMC: stale high anchor vs fresh low payout_sup.
    need_anchor=0
    if [[ -z "$session_anchor" ]]; then
      need_anchor=1
    elif python3 -c "import sys;sys.exit(0 if float(sys.argv[1])+1e-9<float(sys.argv[2]) else 1)" "$payout_sup" "$session_anchor"; then
      need_anchor=1
      echo "[settle-sup] session-anchor ${worker_id}: re-anchor ${session_anchor} -> ${payout_sup} (another coordinator reset)"
    fi
    if [[ "$need_anchor" == "1" ]]; then
      session_anchor="$payout_sup"
      echo "[settle-sup] session-anchor ${worker_id}: settled_sup ${already_sup} kept; anchor=${session_anchor} (coordinator counter reset)"
      tmp="$(mktemp)"
      if ! jq --arg wid "$worker_id" --arg anchor "$session_anchor" --argjson ts "$(date +%s)" \
        '(.workers[$wid] = ((.workers[$wid] // {}) + {session_anchor_sup:($anchor|tonumber),rebased_sup_unix:$ts}))' \
        "$STATE_FILE" >"$tmp"; then
        echo "[settle-sup] session-anchor write failed for ${worker_id}" >&2
        continue
      fi
      mv "$tmp" "$STATE_FILE"
      settle_write_session_anchor_sup "$worker_id" "$session_anchor"
    fi
    baseline_sup="$session_anchor"
  else
    if [[ -n "$session_anchor" ]]; then
      tmp="$(mktemp)"
      jq --arg wid "$worker_id" 'del(.workers[$wid].session_anchor_sup)|del(.workers[$wid].rebased_sup_unix)' "$STATE_FILE" >"$tmp" \
        && mv "$tmp" "$STATE_FILE" || true
      settle_write_session_anchor_sup "$worker_id" clear
    fi
    baseline_sup="$already_sup"
  fi
  delta_sup="$(python3 - "$payout_sup" "$baseline_sup" <<'PY'
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
      already_now="$(settle_read_settled_sup "$worker_id")"
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
      settle_write_settled_sup "$worker_id" "$new_settled" "$pending_addr" || exit 1
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
  curl_fail=0
  for attempt in 1 2 3 4 5; do
    set +e
    resp="$(curl -sS --max-time "$CURL_MAX_TIME" -X POST "${CHAIN_BASE}/api/sup/mint" \
      -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "$(jq -nc --arg to "$to_addr" --argjson amt "$delta_sup" --arg memo "$memo" \
        '{to:$to, amount_sup: ($amt|tonumber), memo:$memo}')" 2>/dev/null)"
    curl_rc=$?
    set -e
    if [[ "$curl_rc" -ne 0 ]]; then
      curl_fail=1
      echo "[settle-sup] WARN ${worker_id}: curl rc=${curl_rc} (keeping pending_sup — ambiguous)" >&2
      sleep $((attempt * 2))
      continue
    fi
    ok="$(jq -r '.ok // false' <<<"$resp" 2>/dev/null || echo false)"
    if [[ "$ok" == "true" ]]; then
      curl_fail=0
      break
    fi
    if [[ "$resp" == *"database is locked"* || "$resp" == *"SQLITE_BUSY"* ]]; then
      sleep $((attempt * 2))
      continue
    fi
    # Explicit reject from API — safe to drop pending and retry later.
    curl_fail=0
    break
  done
  if [[ "$curl_fail" -eq 1 ]]; then
    echo "[settle-sup] ERROR ${worker_id}: ambiguous mint (pending_sup kept)" >&2
    continue
  fi
  if [[ "$ok" != "true" ]]; then
    echo "[settle-sup] ERROR ${worker_id}: $resp" >&2
    # Clear pending only on definitive non-ok JSON (not transport failure).
    tmp="$(mktemp)"
    jq --arg wid "$worker_id" 'del(.workers[$wid].pending_sup)' "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
    continue
  fi
  new_settled="$(python3 - "$already_sup" "$delta_sup" <<'PY'
import sys
print(f"{float(sys.argv[1])+float(sys.argv[2]):.12f}")
PY
)"
  # Advance session floor to current accrued so the next tick only pays new growth.
  new_anchor="$payout_sup"
  tmp="$(mktemp)"
  if [[ "$rebase" == "1" ]]; then
    jq --arg wid "$worker_id" --arg addr "$to_addr" --argjson settled "$new_settled" --argjson anchor "$new_anchor" \
      '.workers[$wid].settled_sup = $settled
       | .workers[$wid].payout_address = $addr
       | .workers[$wid].session_anchor_sup = $anchor
       | del(.workers[$wid].pending_sup)' \
      "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
    settle_write_session_anchor_sup "$worker_id" "$new_anchor"
  else
    jq --arg wid "$worker_id" --arg addr "$to_addr" --argjson settled "$new_settled" \
      '.workers[$wid].settled_sup = $settled | .workers[$wid].payout_address = $addr | del(.workers[$wid].pending_sup) | del(.workers[$wid].session_anchor_sup)' \
      "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
    settle_write_session_anchor_sup "$worker_id" clear
  fi
  settle_write_settled_sup "$worker_id" "$new_settled" "$to_addr" || exit 1
  settled_any=1
  echo "[settle-sup] minted ${worker_id} -> ${to_addr} delta=${delta_sup} SUP"
done < <(jq -r 'to_entries[] | @base64' <<<"$workers_json")

if [[ "$settled_any" == "1" ]]; then
  echo "[settle-sup] done"
else
  echo "[settle-sup] nothing to settle"
fi
