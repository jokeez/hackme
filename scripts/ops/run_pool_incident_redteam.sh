#!/usr/bin/env bash
# Post-incident pool red-team: live probes against prod coordinator + chain.
# Non-destructive negative tests + invariant checks after hdssh01 abuse closure.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

SITE="${SITE:-https://hackme.tech}"
COORD="${COORD_URL:-${SITE}/pool/coordinator}"
CHAIN="${CHAIN_BASE:-${SITE}}"
ABUSER_WALLET="${ABUSER_WALLET:-HMC-9e4e0f72e75deb59}"
ABUSER_WORKER="${ABUSER_WORKER:-worker-hdssh01-public-rust}"
ABUSER_IP="${ABUSER_IP:-104.251.226.83}"
OUT_DIR="${OUT_DIR:-$ROOT/reports/gates/pool_incident_redteam_$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$OUT_DIR"
RESULTS="$OUT_DIR/results.jsonl"
: >"$RESULTS"

COORD_ADMIN="${HACKME_COORDINATOR_ADMIN_TOKEN:-${COORD_ADMIN_TOKEN:-}}"
NODE_ADMIN="${HACKME_ADMIN_TOKEN:-${ADMIN_TOKEN:-}}"
WORKER_TOK="${HACKME_COORDINATOR_WORKER_TOKEN:-${COORD_WORKER_TOKEN:-}}"
[[ -n "$COORD_ADMIN" ]] || [[ ! -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]] || \
  COORD_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
[[ -n "$NODE_ADMIN" ]] || [[ ! -f "$ROOT/.secrets/hackme_admin_token" ]] || \
  NODE_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token")"
[[ -n "$WORKER_TOK" ]] || [[ ! -f "$ROOT/.secrets/hackme_coordinator_worker_token" ]] || \
  WORKER_TOK="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_worker_token")"

pass() { jq -nc --arg id "$1" --arg d "$2" '{id:$id,verdict:"pass",detail:$d}' >>"$RESULTS"; echo "[pass] $1 — $2"; }
fail() { jq -nc --arg id "$1" --arg d "$2" '{id:$id,verdict:"fail",detail:$d}' >>"$RESULTS"; echo "[fail] $1 — $2" >&2; FAILS=$((FAILS+1)); }
FAILS=0

http_code() {
  local method="$1" url="$2" body="${3:-}" token="${4:-}"
  local extra=()
  [[ -n "$token" ]] && extra+=(-H "X-Hackme-Admin-Token: $token")
  if [[ -n "$body" ]]; then
    curl -sS --max-time 20 -o "$OUT_DIR/last.json" -w '%{http_code}' -X "$method" \
      -H "Content-Type: application/json" "${extra[@]}" -d "$body" "$url" || echo 000
  else
    curl -sS --max-time 20 -o "$OUT_DIR/last.json" -w '%{http_code}' -X "$method" \
      "${extra[@]}" "$url" || echo 000
  fi
}

echo "[pool-redteam] COORD=$COORD CHAIN=$CHAIN OUT=$OUT_DIR"

# --- 1. Abuser balances zero ---
addr="$(curl -fsS --max-time 20 "${CHAIN}/api/address/${ABUSER_WALLET}")"
hmc="$(jq -r '.balance_hmc // 0' <<<"$addr")"
sup="$(jq -r '.balance_sup // 0' <<<"$addr")"
if python3 - "$hmc" "$sup" <<'PY'
import sys
h=float(sys.argv[1]); s=float(sys.argv[2])
sys.exit(0 if h < 1e-9 and s < 1e-9 else 1)
PY
then pass "abuser-balances-zero" "HMC=$hmc SUP=$sup"
else fail "abuser-balances-zero" "HMC=$hmc SUP=$sup (expected 0)"
fi

# --- 2. Abuser not in pool ledger ---
bw="$(curl -fsS --max-time 20 "${COORD}/api/work/by-wallet?address=${ABUSER_WALLET}")"
wc="$(jq -r '.workers_count // 0' <<<"$bw")"
if [[ "$wc" == "0" ]]; then pass "abuser-pool-ledger-empty" "workers_count=0"
else fail "abuser-pool-ledger-empty" "workers_count=$wc"
fi

# --- 3. Coordinator stats: no abuser row (needs admin) ---
if [[ -n "$COORD_ADMIN" ]]; then
  st="$(curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $COORD_ADMIN" "${COORD}/api/work/stats?details=1")"
  if jq -e --arg w "$ABUSER_WORKER" '.workers[$w] == null' <<<"$st" >/dev/null; then
    pass "abuser-coord-row-gone" "no worker row"
  else
    fail "abuser-coord-row-gone" "worker still present"
  fi
  tp="$(jq -r '.total_payout_hmc // 0' <<<"$st")"
  if python3 - "$tp" <<'PY'
import sys
sys.exit(0 if float(sys.argv[1]) < 500 else 1)
PY
  then pass "pool-total-sane" "total_payout_hmc=$tp"
  else fail "pool-total-sane" "total_payout_hmc=$tp suspiciously high"
  fi
else
  fail "coord-admin-missing" "set COORD admin token for details=1 checks"
fi

# --- 4. Unauthenticated mutating endpoints rejected ---
for case in \
  "claim-no-auth|POST|${COORD}/api/work/claim|{\"worker_id\":\"rt-probe\",\"batch_size\":1000}" \
  "submit-no-auth|POST|${COORD}/api/work/submit|{\"worker_id\":\"rt-probe\"}" \
  "revoke-no-auth|POST|${COORD}/api/work/admin/revoke-worker|{\"worker_id\":\"x\"}" \
  "sup-burn-no-auth|POST|${CHAIN}/api/sup/burn|{\"from\":\"${ABUSER_WALLET}\"}" \
  "tx-send-no-auth|POST|${CHAIN}/api/tx/send|{\"tx_type\":\"transfer_v1\"}"; do
  IFS='|' read -r id method url body <<<"$case"
  code="$(http_code "$method" "$url" "$body" "")"
  if [[ "$code" == "401" || "$code" == "403" ]]; then pass "$id" "HTTP $code"
  else fail "$id" "HTTP $code (want 401/403)"
  fi
done

# --- 5. stats details requires auth ---
code="$(http_code GET "${COORD}/api/work/stats?details=1" "" "")"
if [[ "$code" == "401" || "$code" == "403" ]]; then pass "stats-details-auth" "HTTP $code"
else fail "stats-details-auth" "HTTP $code"
fi

# --- 6. Oversized batch rejected (worker token) ---
if [[ -n "$WORKER_TOK" ]]; then
  huge='137000000000'
  code="$(http_code POST "${COORD}/api/work/claim" "{\"worker_id\":\"rt-batch-probe\",\"batch_size\":$huge}" "$WORKER_TOK")"
  reason="$(jq -r '.reason // ""' "$OUT_DIR/last.json" 2>/dev/null || true)"
  if [[ "$code" == "400" || "$code" == "403" || "$code" == "429" ]] && [[ "$reason" == *"batch"* || "$reason" == *"large"* || "$reason" == *"ban"* ]]; then
    pass "oversized-batch-blocked" "HTTP $code reason=$reason"
  elif [[ "$code" == "400" || "$code" == "403" ]]; then
    pass "oversized-batch-blocked" "HTTP $code reason=$reason"
  else
    fail "oversized-batch-blocked" "HTTP $code reason=$reason"
  fi
  # Abuser worker id permaban probe
  code="$(http_code POST "${COORD}/api/work/claim" "{\"worker_id\":\"${ABUSER_WORKER}\",\"batch_size\":1000}" "$WORKER_TOK")"
  reason="$(jq -r '.reason // ""' "$OUT_DIR/last.json" 2>/dev/null || true)"
  if [[ "$code" == "403" || "$code" == "429" ]] && [[ "$reason" == *"permaban"* || "$reason" == *"ban"* ]]; then pass "abuser-permaban" "HTTP $code $reason"
  else fail "abuser-permaban" "HTTP $code reason=$reason (want banned)"
  fi
else
  fail "worker-token-missing" "cannot test batch/permaban without worker token"
fi

# --- 7. Go unit pack (local) ---
if go test -count=1 -timeout=120s ./cmd/coordinator/ -run 'Abuse|Oversized|Revoke|Batch|clamp' >"$OUT_DIR/go_abuse.log" 2>&1; then
  pass "go-abuse-unit" "cmd/coordinator abuse tests PASS"
else
  fail "go-abuse-unit" "see $OUT_DIR/go_abuse.log"
fi

# --- summary ---
total="$(wc -l <"$RESULTS" | tr -d ' ')"
jq -nc --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --argjson total "$total" --argjson fails "$FAILS" \
  '{captured_at:$ts,total:$total,fails:$fails,status:(if $fails==0 then "PASS" else "FAIL" end)}' \
  >"$OUT_DIR/summary.json"
echo "[pool-redteam] summary: $OUT_DIR/summary.json fails=$FAILS/$total"
[[ "$FAILS" -eq 0 ]]
