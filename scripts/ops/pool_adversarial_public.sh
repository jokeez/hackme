#!/usr/bin/env bash
# Adversarial pool checks (fake blocks, inflated attempts, replay, time-ish cheats).
# Run from any host (e.g. Moscow VPS) against public coordinator.
#
#   COORD_URL=https://hackme.tech/pool/coordinator \
#   COORD_TOKEN=... \  # optional: enables signed-cheating probes
#   bash scripts/ops/pool_adversarial_public.sh
#
# Without COORD_TOKEN: only tests that must fail closed (401/403/409).

set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
COORD_URL="${COORD_URL%/}"
TOKEN="${COORD_TOKEN:-${HACKME_POOL_COORDINATOR_TOKEN:-}}"
if [[ -z "$TOKEN" && -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]]; then
  TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
fi
WORKER="${WORKER_ID:-redteam-msk-01}"
OUT="${OUT_DIR:-$ROOT/reports/adversarial_$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$OUT"
LOG="$OUT/report.txt"
: >"$LOG"
pass=0
fail=0
warn=0

log() { echo "[adv] $*" | tee -a "$LOG"; }
record() {
  local id="$1" expect="$2" got="$3" note="$4"
  got="${got//$'\r'/}"
  local ok=0
  if [[ "$expect" == "$got" ]]; then ok=1; fi
  if [[ "$ok" -eq 0 ]]; then
    local code
    IFS='|' read -ra codes <<<"$expect"
    for code in "${codes[@]}"; do
      if [[ "$code" == "$got" ]]; then ok=1; break; fi
    done
  fi
  if (( ok )); then
    pass=$((pass + 1))
    log "PASS $id: $note (got=$got)"
  else
    fail=$((fail + 1))
    log "FAIL $id: expected=$expect got=$got — $note"
  fi
}

post() {
  local url="$1" body="$2" tok="$3" out="$4"
  local args=(-sS -o "$out" -w '%{http_code}' -X POST -H 'Content-Type: application/json' --max-time 25 -d "$body")
  [[ -n "$tok" ]] && args+=(-H "X-Hackme-Admin-Token: $tok")
  curl "${args[@]}" "$url" 2>/dev/null || echo "000"
}

get_code() {
  local url="$1" tok="$2" out="$3"
  local args=(-sS -o "$out" -w '%{http_code}' --max-time 20)
  [[ -n "$tok" ]] && args+=(-H "X-Hackme-Admin-Token: $tok")
  curl "${args[@]}" "$url" 2>/dev/null || echo "000"
}

log "target=$COORD_URL worker=$WORKER token_set=$([[ -n "$TOKEN" ]] && echo yes || echo no)"
log "host=$(hostname -f 2>/dev/null || hostname) from=$(curl -fsS --max-time 5 https://ifconfig.me 2>/dev/null || echo n/a)"

# --- Must reject without token ---
o="$OUT/unauth_claim.json"
h="$(post "$COORD_URL/api/work/claim" "{\"worker_id\":\"$WORKER\",\"batch_size\":65536}" "" "$o")"
record "unauth-claim" "401|403" "$h" "claim without token"

o="$OUT/unauth_submit.json"
h="$(post "$COORD_URL/api/work/submit" "{\"worker_id\":\"$WORKER\",\"base_nonce\":1,\"batch_size\":1,\"attempts\":1}" "" "$o")"
record "unauth-submit" "401|403" "$h" "submit without token"

o="$OUT/unauth_clear.json"
h="$(post "$COORD_URL/api/work/admin/clear-abuse" '{"all":true}' "" "$o")"
record "unauth-clear-abuse" "401|403" "$h" "clear-abuse without token"

o="$OUT/unauth_stats.json"
h="$(get_code "$COORD_URL/api/work/stats?details=1" "" "$o")"
record "unauth-stats-details" "401|403" "$h" "stats details=1 without token"

# Public node surface (not coordinator)
for path in /api/mining/start /api/genesis /api/tx/send; do
  o="$OUT/node_$(echo "$path" | tr '/' '_').json"
  h="$(post "https://hackme.tech${path}" '{}' "" "$o" 2>/dev/null || echo 000)"
  case "$h" in
    401|403|404)
      pass=$((pass + 1))
      log "PASS node${path}: unauth rejected http=$h"
      ;;
    200|201)
      fail=$((fail + 1))
      log "FAIL node${path}: got $h (should not allow)"
      ;;
    *)
      warn=$((warn + 1))
      log "WARN node${path}: http=$h"
      ;;
  esac
done

if [[ -z "$TOKEN" ]]; then
  log "COORD_TOKEN unset — skipping authenticated cheat probes"
  log "SUMMARY pass=$pass fail=$fail warn=$warn"
  exit $(( fail > 0 ? 1 : 0 ))
fi

# --- Authenticated cheat probes (compromised miner) ---
claim_ok="$OUT/claim_ok.json"
h="$(post "$COORD_URL/api/work/claim" "{\"worker_id\":\"$WORKER\",\"batch_size\":1048576}" "$TOKEN" "$claim_ok")"
if [[ "$h" != "200" ]]; then
  record "claim-ok" "200" "$h" "baseline claim"
  log "SUMMARY pass=$pass fail=$fail warn=$warn"
  exit 1
fi
base="$(jq -r '.base_nonce' "$claim_ok")"
size="$(jq -r '.batch_size' "$claim_ok")"
wid="$(jq -r '.work_id' "$claim_ok")"
mod="$(jq -r '.target_mod' "$claim_ok")"

# Inflated attempts (report 1e12 tries, found=false)
o="$OUT/inflate_attempts.json"
body="$(jq -nc --arg w "$WORKER" --argjson b "$base" --argjson s "$size" --arg wid "$wid" \
  '{worker_id:$w,base_nonce:$b,batch_size:$s,work_id:$wid,attempts:1000000000000,found:false,hashrate_gh_s:99999}')"
h="$(post "$COORD_URL/api/work/submit" "$body" "$TOKEN" "$o")"
reason="$(jq -r '.reason // .error // ""' "$o" 2>/dev/null)"
if [[ "$h" == "200" ]]; then
  acc="$(jq -r '.accepted // false' "$o")"
  pay="$(jq -r '.payout_hmc // 0' "$o")"
  if [[ "$acc" == "true" && "$(python3 - <<PY
p=float("$pay")
print(1 if p>1.0 else 0)
PY
)" == "1" ]]; then
    fail=$((fail+1)); log "FAIL inflate-attempts: huge payout $pay HMC for fake attempts"
  else
    pass=$((pass+1)); log "PASS inflate-attempts: accepted=$acc payout=$pay (capped/policy)"
  fi
else
  pass=$((pass+1)); log "PASS inflate-attempts: rejected http=$h reason=$reason"
fi

# Fake PoH solve (wrong nonce for mod)
o="$OUT/fake_found.json"
body="$(jq -nc --arg w "$WORKER" --argjson b "$base" --argjson s "$size" --arg wid "$wid" \
  '{worker_id:$w,base_nonce:$b,batch_size:$s,work_id:$wid,attempts:$s,found:true,found_nonce:1,result_hash:"deadbeef"}')"
h="$(post "$COORD_URL/api/work/submit" "$body" "$TOKEN" "$o")"
reason="$(jq -r '.reason // ""' "$o" 2>/dev/null)"
case "$reason" in
  found_nonce_invalid*|invalid*|signature_required|result_hash_required*) pass=$((pass+1)); log "PASS fake-found: rejected ($reason)" ;;
  *)
    if [[ "$h" == "200" && "$(jq -r '.accepted' "$o")" == "true" ]]; then
      fail=$((fail+1)); log "FAIL fake-found: accepted invalid solve"
    else
      pass=$((pass+1)); log "PASS fake-found: http=$h reason=$reason"
    fi
    ;;
esac

# Replay closed range
o="$OUT/replay.json"
body="$(jq -nc --arg w "$WORKER" --argjson b "$base" --argjson s "$size" --arg wid "$wid" \
  '{worker_id:$w,base_nonce:$b,batch_size:$s,work_id:$wid,attempts:1000,found:false}')"
h1="$(post "$COORD_URL/api/work/submit" "$body" "$TOKEN" "$o")"
h2="$(post "$COORD_URL/api/work/submit" "$body" "$TOKEN" "$OUT/replay2.json")"
if [[ "$h1" == "200" && "$h2" == "409" ]]; then
  pass=$((pass+1)); log "PASS replay-range: first 200 second 409"
elif [[ "$h2" == "409" ]]; then
  pass=$((pass+1)); log "PASS replay-range: second submit rejected"
else
  warn=$((warn+1)); log "WARN replay: h1=$h1 h2=$h2"
fi

# Work id hijack string
o="$OUT/hijack.json"
body="$(jq -nc --arg w "$WORKER" --argjson b "$base" --argjson s "$size" \
  '{worker_id:$w,base_nonce:$b,batch_size:$s,work_id:"worker-kapa-pc:evil",attempts:1000}')"
h="$(post "$COORD_URL/api/work/submit" "$body" "$TOKEN" "$o")"
record "workid-hijack" "409" "$h" "wrong work_id"

# Stats before/after (payout should not jump absurdly on one inflated submit)
stats="$OUT/stats_after.json"
get_code "$COORD_URL/api/work/stats?details=1" "$TOKEN" "$stats" >/dev/null
pay_total="$(jq -r '.total_payout_hmc // 0' "$stats")"
log "pool total_payout_hmc after probes: $pay_total"

log "SUMMARY pass=$pass fail=$fail warn=$warn out=$OUT"
exit $(( fail > 0 ? 1 : 0 ))
