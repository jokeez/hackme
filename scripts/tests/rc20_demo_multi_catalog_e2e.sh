#!/usr/bin/env bash
# Demo customer: Fund once, Dig once, Hunt several random catalog targets, cancel each, report pickup.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-/tmp/hackme-rc20-multi-$STAMP}"
mkdir -p "$OUT" && chmod 700 "$OUT"
HUB="${HACKME_ORDERS_HUB_URL:-https://hackme.tech}"
BASE="${HACKME_CHAIN_BASE:-https://hackme.tech}"
PUB_FILE="${HACKME_ORDERS_PUBLISH_TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_orders_publish_token}"
TREASURY_SEED="${TREASURY_FUND_SEED_HEX:-$ROOT/.secrets/hackme_treasury_ed25519_seed.hex}"
TREASURY=HMC-719006d93916ad52
DEVFEE=HMC-719006d93916ad52
FUND_HMC="${FUND_HMC:-80}"
DIG_BUDGET="${DIG_BUDGET:-1}"
HUNT_BUDGET="${HUNT_BUDGET:-20}"
WAIT_SEC="${WAIT_RUNS_SEC:-120}"
# Prefer diverse catalog ids (override with TARGETS="jsmn cjson yyjson")
TARGETS="${TARGETS:-jsmn cjson centijson yyjson parson}"

log() { echo "[rc20-multi $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/run.log"; }
die() { log "FAIL: $*"; echo "{\"ok\":false,\"error\":$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$*")}" >"$OUT/VERDICT.json"; exit 1; }
extract_json() { python3 -c 'import pathlib,re,sys; t=pathlib.Path(sys.argv[1]).read_text(); m=re.search(r"\{.*\}",t,re.S); sys.exit(1) if not m else pathlib.Path(sys.argv[2]).write_text(m.group(0)+"\n")' "$1" "$2"; }

[[ -r "$PUB_FILE" && -r "$TREASURY_SEED" ]] || die "secrets missing"
go build -o "$OUT/show_node_addr" ./tools/show_node_addr
go build -o "$OUT/sendtransfer" ./cmd/sendtransfer
go build -o "$OUT/rc20_pool_client" ./tools/rc20_pool_client

CUST="$OUT/customer"; mkdir -p "$CUST" && chmod 700 "$CUST"
python3 -c "import secrets,os; open('$CUST/node_ed25519.seed','w').write(secrets.token_hex(32)); os.chmod('$CUST/node_ed25519.seed',0o600)"
OWNER="$("$OUT/show_node_addr" "$CUST")"
log "customer=$OWNER targets=$TARGETS fund=$FUND_HMC"

TMP_T=$(mktemp -d) && chmod 700 "$TMP_T"
tr -d '\r\n' <"$TREASURY_SEED" >"$TMP_T/node_ed25519.seed" && chmod 600 "$TMP_T/node_ed25519.seed"
[[ "$("$OUT/show_node_addr" "$TMP_T")" == "$TREASURY" ]] || die "treasury mismatch"
TOPUP=$(python3 -c "print($FUND_HMC + 0.1)")
HACKME_DATA_DIR="$TMP_T" "$OUT/sendtransfer" -base "$BASE" -to "$OWNER" -amount-hmc "$TOPUP" -memo rc20_multi_topup >"$OUT/topup.raw" 2>"$OUT/topup.err" || die "topup $(cat "$OUT/topup.err")"
extract_json "$OUT/topup.raw" "$OUT/topup.json"
TOP_TX=$(python3 -c 'import json; print(json.load(open("'"$OUT/topup.json"'"))["tx_hash"])')
for i in $(seq 1 50); do
  st=$(curl -fsS --max-time 10 "$BASE/api/tx/$TOP_TX" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))' 2>/dev/null || true)
  [[ "$st" == "included" || "$st" == "applied" || "$st" == "confirmed" ]] && break
  sleep 2
done
rm -rf "$TMP_T"

MEMO="orders_fund_v1:${OWNER}"
HACKME_DATA_DIR="$CUST" "$OUT/sendtransfer" -base "$BASE" -to "$DEVFEE" -amount-hmc "$FUND_HMC" -memo "$MEMO" >"$OUT/fund.raw" 2>"$OUT/fund.err" || die "fund"
extract_json "$OUT/fund.raw" "$OUT/fund.json"
TX=$(python3 -c 'import json; print(json.load(open("'"$OUT/fund.json"'"))["tx_hash"])')
for i in $(seq 1 50); do
  st=$(curl -fsS --max-time 10 "$BASE/api/tx/$TX" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))' 2>/dev/null || true)
  log "fund status=$st"
  [[ "$st" == "included" || "$st" == "applied" || "$st" == "confirmed" ]] && break
  sleep 2
done

export HACKME_ORDERS_HUB_URL="$HUB" HACKME_DATA_DIR="$CUST"
export HACKME_POOL_ORDERS_PUBLISH_TOKEN; HACKME_POOL_ORDERS_PUBLISH_TOKEN="$(tr -d '\r\n' <"$PUB_FILE")"
"$OUT/rc20_pool_client" fund-confirm --tx "$TX" --amount-hmc "$FUND_HMC" >"$OUT/fund_confirm.json" 2>"$OUT/fund_confirm.err" || die "fund confirm"
PRE0=$("$OUT/rc20_pool_client" prepaid | python3 -c 'import json,sys; print(json.load(sys.stdin).get("spendable_hmc",""))')
log "prepaid_after_fund=$PRE0"

RESULTS=()
run_job() {
  local kind="$1" id="" tok="" runs=0
  shift
  if [[ "$kind" == "dig" ]]; then
    "$OUT/rc20_pool_client" dig --budget-hmc "$DIG_BUDGET" --title "multi-dig-$STAMP" >"$OUT/dig.json" 2>"$OUT/dig.err" || die "dig"
    id=$(python3 -c 'import json,sys; j=json.load(open(sys.argv[1])); print(j.get("campaign_id") or "")' "$OUT/dig.json")
    tok=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("customer_report_token") or "")' "$OUT/dig.json")
  else
    local target="$1"
    "$OUT/rc20_pool_client" hunt --budget-hmc "$HUNT_BUDGET" --target "$target" --shards 32 --title "multi-hunt-$target-$STAMP" \
      >"$OUT/hunt-$target.json" 2>"$OUT/hunt-$target.err" || die "hunt $target: $(tail -c 300 "$OUT/hunt-$target.err"; tail -c 300 "$OUT/hunt-$target.json")"
    id=$(python3 -c 'import json,sys; j=json.load(open(sys.argv[1])); print(j.get("campaign_id") or (j.get("campaign") or {}).get("id") or "")' "$OUT/hunt-$target.json")
    tok=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("customer_report_token") or "")' "$OUT/hunt-$target.json")
  fi
  [[ -n "$id" ]] || die "no campaign id for $kind"
  log "$kind id=$id waiting pickup"
  local deadline=$((SECONDS + WAIT_SEC))
  while (( SECONDS < deadline )); do
    if [[ -n "$tok" ]]; then
      local prog
      prog=$(curl -fsS --max-time 12 -H "X-Hackme-Report-Token: $tok" "$HUB/api/fuzz/campaigns/$id/pulse" 2>/dev/null || true)
      if [[ -n "$prog" ]]; then
        printf '%s' "$prog" >"$OUT/pulse-$id.json"
        runs=$(python3 -c 'import json,sys
j=json.load(open(sys.argv[1]))
def n(o):
  if not isinstance(o,dict): return 0
  for k in ("runs_done","shards_done","runs"):
    if k in o:
      try: return int(o[k] or 0)
      except Exception: return 0
  return 0
print(n(j) or n(j.get("summary") or {}) or n(j.get("escrow") or {}) or n(j.get("campaign") or {}))' "$OUT/pulse-$id.json" 2>/dev/null || echo 0)
      fi
    fi
    log "$kind progress=$runs"
    (( runs >= 1 )) && break
    sleep 6
  done
  local cfile="$OUT/cancel-${id}.json"
  "$OUT/rc20_pool_client" cancel --campaign-id "$id" >"$cfile" 2>"$OUT/cancel-${id}.err" || die "cancel $id ($(tail -c 200 "$OUT/cancel-${id}.err" 2>/dev/null || true))"
  local esc runs_esc prepaid
  esc=$(python3 -c 'import json,sys; print((json.load(open(sys.argv[1])).get("escrow") or {}).get("status",""))' "$cfile")
  runs_esc=$(python3 -c 'import json,sys; print((json.load(open(sys.argv[1])).get("escrow") or {}).get("runs_done",0))' "$cfile")
  prepaid=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("spendable_hmc",""))' "$cfile")
  # Authoritative private report must come from hub (token), not a local empty mirror.
  local report_ok=0
  if [[ -n "$tok" ]]; then
    if curl -fsS --max-time 20 -H "X-Hackme-Report-Token: $tok" \
         -H "Accept: application/json" \
         "$HUB/api/fuzz/campaigns/$id/report?format=json&limit=20" >"$OUT/report-$id.json" 2>"$OUT/report-$id.err"; then
      report_ok=$(python3 -c 'import json,sys
j=json.load(open(sys.argv[1]))
print(1 if j.get("ok") or j.get("report_version") or j.get("verdict_card") or j.get("gate") else 0)' "$OUT/report-$id.json" 2>/dev/null || echo 0)
    fi
  fi
  [[ "$report_ok" == "1" ]] || die "$kind report fetch failed id=$id ($(tail -c 200 "$OUT/report-$id.err" 2>/dev/null || true))"
  log "$kind done escrow=$esc runs_esc=$runs_esc prepaid=$prepaid report_ok=$report_ok"
  RESULTS+=("$kind:$id:pulse=$runs:esc_runs=$runs_esc:escrow=$esc:report=$report_ok")
}

run_job dig
for t in $TARGETS; do
  # Orders publish rate limit (hub) trips if Hunt creates are back-to-back.
  sleep "${HUNT_GAP_SEC:-12}"
  run_job hunt "$t"
done

PRE_END=$("$OUT/rc20_pool_client" prepaid | python3 -c 'import json,sys; print(json.load(sys.stdin).get("spendable_hmc",""))')
python3 - <<PY >"$OUT/VERDICT.json"
import json
jobs = """$(printf '%s\n' "${RESULTS[@]}")""".strip().splitlines()
pickup = False
reports = True
for j in jobs:
  parts = dict(p.split("=",1) for p in j.split(":") if "=" in p)
  try:
    if int(parts.get("esc_runs","0") or 0) >= 1:
      pickup = True
  except Exception:
    pass
  if parts.get("report") != "1":
    reports = False
ok = pickup and reports and len(jobs) > 0
v = {
  "ok": ok,
  "stamp": "$STAMP",
  "owner": "$OWNER",
  "fund_hmc": float("$FUND_HMC"),
  "prepaid_after_fund": "$PRE0",
  "prepaid_end": "$PRE_END",
  "jobs": jobs,
  "pickup_any": pickup,
  "private_report_ok": reports,
  "targets": "$TARGETS".split(),
}
print(json.dumps(v, indent=2))
PY
cat "$OUT/VERDICT.json"
python3 -c 'import json,sys; v=json.load(open("'"$OUT/VERDICT.json"'"));
print("PASS multi catalog demo" if v.get("ok") else "FAIL");
sys.exit(0 if v.get("ok") else 1)'
log "done OUT=$OUT"
