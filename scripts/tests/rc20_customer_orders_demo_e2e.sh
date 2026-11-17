#!/usr/bin/env bash
# Demo E2E as a real Orders customer: build lean pack → start local Orders →
# treasury topup → Fund Orders → Dig (pool via hub) → wait worker runs → cancel → report.
# Does NOT ship prepaid hub secret (rc20). Secrets never printed.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-/tmp/hackme-rc20-demo-$STAMP}"
PACK="${PACK:-$OUT/orders-pack}"
PORT="${HACKME_ORDERS_PORT:-18088}"
BASE_LOCAL="http://127.0.0.1:${PORT}"
HUB="${HACKME_ORDERS_HUB_URL:-https://hackme.tech}"
CHAIN="${HACKME_CHAIN_BASE:-https://hackme.tech}"
PUB_FILE="${HACKME_ORDERS_PUBLISH_TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_orders_publish_token}"
TREASURY_SEED="${TREASURY_FUND_SEED_HEX:-$ROOT/.secrets/hackme_treasury_ed25519_seed.hex}"
TREASURY=HMC-719006d93916ad52
FUND_HMC="${FUND_HMC:-15}"
BUDGET_HMC="${BUDGET_HMC:-1}"
WAIT_RUNS_SEC="${WAIT_RUNS_SEC:-300}"
VERSION="${VERSION:-0.1.0-rc20-demo}"

mkdir -p "$OUT" "$PACK" && chmod 700 "$OUT"
log() { echo "[rc20-demo $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/run.log"; }
die() { log "FAIL: $*"; printf '{"ok":false,"error":%s}\n' "$(printf '%s' "$*" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')" >"$OUT/VERDICT.json"; exit 1; }
json_field() { python3 -c 'import json,sys; j=json.load(open(sys.argv[1])); print(j.get(sys.argv[2],"") if not isinstance(j.get(sys.argv[2]),dict) else j.get(sys.argv[2]))' "$1" "$2" 2>/dev/null || true; }

[[ -r "$PUB_FILE" ]] || die "publish token file missing"
[[ -r "$TREASURY_SEED" ]] || die "treasury seed missing"

cleanup() {
  if [[ -f "$OUT/orders.pid" ]]; then
    kill "$(cat "$OUT/orders.pid")" 2>/dev/null || true
  fi
}
trap cleanup EXIT

log "build customer Orders binary → $PACK"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo nogit)"
LDFLAGS="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
go build -trimpath -ldflags "$LDFLAGS" -o "$PACK/hackme" ./
go build -o "$OUT/show_node_addr" ./tools/show_node_addr
go build -o "$OUT/sendtransfer" ./cmd/sendtransfer
cp -f "$ROOT/scripts/release/linux/start_hackme_orders.sh" "$PACK/start_hackme_orders.sh"
chmod +x "$PACK/start_hackme_orders.sh" "$PACK/hackme"
install -m 600 "$PUB_FILE" "$PACK/pool.orders.token"
# Explicitly NO prepaid hub secret in customer pack
rm -f "$PACK/pool.orders.prepaid_hub.secret"
printf '%s\n' "REPLACE_WITH_ORDERS_PREPAID_HUB_SECRET" >"$PACK/pool.orders.prepaid_hub.secret.example"
cat >"$OUT/BUILD_INFO.txt" <<EOF
app=HackMe
profile=orders-customer-rc20-demo
version=${VERSION}
commit=${COMMIT}
hub=${HUB}
built_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
prepaid_hub_secret_shipped=0
EOF

# Isolated XDG so we do not touch the user's live desktop on :8080
export XDG_CONFIG_HOME="$OUT/xdg-config"
export XDG_DATA_HOME="$OUT/xdg-data"
mkdir -p "$XDG_CONFIG_HOME/hackme" "$XDG_DATA_HOME/hackme"
export HACKME_ORDERS_PORT="$PORT"
export HACKME_ORDERS_HUB_URL="$HUB"
export SKIP_TOOLCHAINS=1

log "start Orders on $BASE_LOCAL"
(
  cd "$PACK"
  # --start only: do not xdg-open browser in headless demo
  nohup env HACKME_ORDERS_PORT="$PORT" HACKME_ORDERS_HUB_URL="$HUB" SKIP_TOOLCHAINS=1 \
    bash ./start_hackme_orders.sh --start >"$OUT/orders.stdout" 2>"$OUT/orders.stderr" &
  echo $! >"$OUT/orders.launcher.pid"
)
# start script may daemonize; find real hackme pid
for i in $(seq 1 60); do
  if curl -fsS --max-time 2 "$BASE_LOCAL/api/status?lite=1" >/dev/null 2>&1 \
    || curl -fsS --max-time 2 "$BASE_LOCAL/api/health" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl -fsS --max-time 5 "$BASE_LOCAL/api/status?lite=1" >"$OUT/status.json" 2>/dev/null \
  || curl -fsS --max-time 5 "$BASE_LOCAL/api/health" >"$OUT/status.json" \
  || die "Orders node did not come up on $BASE_LOCAL (see orders.stderr)"
# capture pid listening on PORT
pid="$(ss -ltnp 2>/dev/null | python3 -c "
import sys,re
for line in sys.stdin:
  if ':$PORT' in line:
    m=re.search(r'pid=(\d+)', line)
    if m: print(m.group(1)); break
" || true)"
[[ -n "$pid" ]] && echo "$pid" >"$OUT/orders.pid"
log "Orders up pid=${pid:-?} status=$(head -c 120 "$OUT/status.json")"

# Admin/local-auth for mutating routes
AUTH_HDR=()
if tok="$(python3 - <<'PY' 2>/dev/null
import json,urllib.request,os
base=os.environ.get("BASE","")
# try desktop local-auth cookie/token endpoints used by dashboard
for path in ("/api/desktop/local-auth","/api/local-auth"):
  try:
    with urllib.request.urlopen(base+path, timeout=5) as r:
      j=json.load(r)
      t=j.get("admin_token") or j.get("token") or ""
      if t: print(t); raise SystemExit
  except Exception:
    pass
PY
)" 2>/dev/null; then
  :
fi
BASE="$BASE_LOCAL" python3 - <<'PY' >"$OUT/local_auth.json" 2>"$OUT/local_auth.err" || true
import json,urllib.request,os
base=os.environ["BASE"]
out={}
for path in ("/api/desktop/local-auth","/api/local-auth","/api/status"):
  try:
    with urllib.request.urlopen(base+path, timeout=8) as r:
      out[path]=json.load(r)
  except Exception as e:
    out[path]={"error":str(e)}
print(json.dumps(out, indent=2))
PY
ADMIN="$(python3 -c '
import json,os,glob
j=json.load(open("'"$OUT/local_auth.json"'"))
for path,v in j.items():
  if not isinstance(v,dict): continue
  for k in ("admin_token","token","desktop_admin_token"):
    if v.get(k): print(v[k]); raise SystemExit
# fallback: env.orders written by start script
import pathlib
for p in pathlib.Path("'"$XDG_CONFIG_HOME"'").rglob("env.orders"):
  for line in p.read_text().splitlines():
    if line.startswith("HACKME_ADMIN_TOKEN=") or line.startswith("HACKME_REQUIRE_ADMIN_TOKEN="):
      print(line.split("=",1)[1].strip()); raise SystemExit
' 2>/dev/null || true)"
# Also read from data dir seed address via show_node_addr
DATA_DIR="$(python3 -c '
import pathlib
c=pathlib.Path("'"$XDG_CONFIG_HOME"'")
d=pathlib.Path("'"$XDG_DATA_HOME"'")
for p in list(c.rglob("env.orders"))+list(d.rglob("env.orders")):
  for line in p.read_text().splitlines():
    if line.startswith("HACKME_DATA_DIR="):
      print(line.split("=",1)[1].strip()); raise SystemExit
# default under xdg
print("'"$XDG_DATA_HOME"'/hackme/data")
' 2>/dev/null || echo "$XDG_DATA_HOME/hackme/data")"
# find actual data dir with seed
if [[ ! -f "$DATA_DIR/node_ed25519.seed" ]]; then
  found="$(find "$XDG_DATA_HOME" "$XDG_CONFIG_HOME" -name 'node_ed25519.seed' 2>/dev/null | head -1 || true)"
  [[ -n "$found" ]] && DATA_DIR="$(dirname "$found")"
fi
[[ -f "$DATA_DIR/node_ed25519.seed" ]] || die "customer seed missing under $DATA_DIR"
OWNER="$("$OUT/show_node_addr" "$DATA_DIR")"
log "customer=$OWNER data=$DATA_DIR admin_set=$([ -n "$ADMIN" ] && echo yes || echo no)"
echo "$OWNER" >"$OUT/customer_addr.txt"

# Discover admin from env.orders if still empty
if [[ -z "$ADMIN" ]]; then
  ADMIN="$(grep -E '^HACKME_(REQUIRE_)?ADMIN_TOKEN=' "$XDG_CONFIG_HOME/hackme/env.orders" 2>/dev/null | head -1 | cut -d= -f2- | tr -d '\r' || true)"
fi
[[ -n "$ADMIN" ]] || ADMIN="$(grep -E '^HACKME_(REQUIRE_)?ADMIN_TOKEN=' "$(dirname "$DATA_DIR")/../config/hackme/env.orders" 2>/dev/null | head -1 | cut -d= -f2- || true)"
# start script often puts admin in data/.env or generates — scrape orders.stderr
if [[ -z "$ADMIN" ]]; then
  ADMIN="$(grep -oE 'admin[_-]?token[=: ]+[A-Za-z0-9+/=_-]{16,}' "$OUT/orders.stderr" 2>/dev/null | tail -1 | awk '{print $NF}' || true)"
fi

hdr_admin() {
  if [[ -n "${ADMIN:-}" ]]; then
    echo -H "X-Hackme-Admin-Token: $ADMIN" -H "Authorization: Bearer $ADMIN"
  fi
}

# 1) Treasury tops up customer on-chain
TOPUP=$(python3 -c "print($FUND_HMC + 0.05)")
log "treasury topup $TOPUP HMC → $OWNER"
TMP_T=$(mktemp -d) && chmod 700 "$TMP_T"
tr -d '\r\n' <"$TREASURY_SEED" >"$TMP_T/node_ed25519.seed" && chmod 600 "$TMP_T/node_ed25519.seed"
FROM="$("$OUT/show_node_addr" "$TMP_T")"
[[ "$FROM" == "$TREASURY" ]] || die "treasury mismatch"
HACKME_DATA_DIR="$TMP_T" "$OUT/sendtransfer" -base "$CHAIN" -to "$OWNER" -amount-hmc "$TOPUP" -memo "rc20_demo_topup" \
  >"$OUT/topup.raw" 2>"$OUT/topup.err" || die "topup failed"
python3 -c 'import pathlib,re,sys; t=pathlib.Path("'"$OUT/topup.raw"'").read_text(); m=re.search(r"\{.*\}",t,re.S); pathlib.Path("'"$OUT/topup.json"'").write_text(m.group(0)+"\n") if m else sys.exit(1)'
TOP_TX=$(python3 -c 'import json; print(json.load(open("'"$OUT/topup.json"'")).get("tx_hash",""))')
[[ -n "$TOP_TX" ]] || die "topup missing tx_hash"
for i in $(seq 1 50); do
  st=$(curl -fsS --max-time 10 "$CHAIN/api/tx/$TOP_TX" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))' 2>/dev/null || true)
  log "topup status=$st"
  [[ "$st" == "included" || "$st" == "applied" || "$st" == "confirmed" ]] && break
  sleep 2
done
rm -rf "$TMP_T"

# 2) Fund Orders via customer local API (same as dashboard Fund Orders)
log "customer Fund Orders $FUND_HMC via $BASE_LOCAL/api/orders/fund"
FUND_ARGS=(-sS --max-time 120 -X POST -H 'Content-Type: application/json')
if [[ -n "${ADMIN:-}" ]]; then
  FUND_ARGS+=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Authorization: Bearer $ADMIN")
fi
curl "${FUND_ARGS[@]}" -d "{\"amount_hmc\":$FUND_HMC}" "$BASE_LOCAL/api/orders/fund" \
  >"$OUT/fund.json" 2>"$OUT/fund.err" || die "fund POST failed"
TX=$(python3 -c 'import json; j=json.load(open("'"$OUT/fund.json"'")); print(j.get("tx_hash") or (j.get("transfer") or {}).get("tx_hash") or "")')
[[ -n "$TX" ]] || die "fund missing tx_hash body=$(head -c 300 "$OUT/fund.json")"
echo "$TX" >"$OUT/fund_tx_hash.txt"
for i in $(seq 1 50); do
  st=$(curl -fsS --max-time 10 "$CHAIN/api/tx/$TX" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))' 2>/dev/null || true)
  log "fund status=$st"
  [[ "$st" == "included" || "$st" == "applied" || "$st" == "confirmed" ]] && break
  sleep 2
done

log "Fund confirm"
curl "${FUND_ARGS[@]}" -d "{\"tx_hash\":\"$TX\"}" "$BASE_LOCAL/api/orders/fund/confirm" \
  >"$OUT/fund_confirm.json" 2>"$OUT/fund_confirm.err" || die "fund confirm failed"
python3 -c 'import json; j=json.load(open("'"$OUT/fund_confirm.json"'")); assert j.get("ok") or j.get("funded"), j' \
  || die "fund confirm not ok: $(head -c 400 "$OUT/fund_confirm.json")"

# Hub prepaid (authoritative) via publish+owner proof client
go build -o "$OUT/rc20_pool_client" ./tools/rc20_pool_client
export HACKME_ORDERS_HUB_URL="$HUB"
export HACKME_POOL_ORDERS_PUBLISH_TOKEN
HACKME_POOL_ORDERS_PUBLISH_TOKEN="$(tr -d '\r\n' <"$PUB_FILE")"
export HACKME_DATA_DIR="$DATA_DIR"
"$OUT/rc20_pool_client" prepaid >"$OUT/hub_prepaid.json" 2>"$OUT/hub_prepaid.err" \
  || die "hub prepaid: $(cat "$OUT/hub_prepaid.err") $(cat "$OUT/hub_prepaid.json")"
PRE_BEFORE=$(python3 -c 'import json; print(json.load(open("'"$OUT/hub_prepaid.json"'")).get("spendable_hmc",""))')
log "hub_prepaid_after_fund=$PRE_BEFORE"

# 3) Dig via customer local security-audit (proxies to hub when pool+hub URL)
log "Dig Start budget=$BUDGET_HMC pool_distributed"
DIG_BODY=$(python3 -c "import json; print(json.dumps({
  'title': 'rc20-demo-$STAMP',
  'dig_package': 'scan',
  'guard_pack': 'overflow_smoke',
  'budget_hmc': $BUDGET_HMC,
  'pool_distributed': True,
}))")
curl "${FUND_ARGS[@]}" -d "$DIG_BODY" "$BASE_LOCAL/api/security-audit" \
  >"$OUT/dig.json" 2>"$OUT/dig.err" || true
# some builds use POST body field names differently — retry minimal
if ! python3 -c 'import json; j=json.load(open("'"$OUT/dig.json"'")); assert j.get("ok") or j.get("campaign_id") or j.get("campaign")' 2>/dev/null; then
  log "dig first response: $(head -c 240 "$OUT/dig.json")"
  curl "${FUND_ARGS[@]}" -d "$DIG_BODY" "$BASE_LOCAL/api/fuzz/campaigns" \
    >"$OUT/dig.json" 2>"$OUT/dig.err" || die "dig failed"
fi
CID=$(python3 -c 'import json; j=json.load(open("'"$OUT/dig.json"'")); print(j.get("campaign_id") or (j.get("campaign") or {}).get("id") or "")')
[[ -n "$CID" ]] || die "no campaign_id: $(head -c 500 "$OUT/dig.json")"
echo "$CID" >"$OUT/campaign_id.txt"
log "campaign_id=$CID hub_pool=$(python3 -c 'import json; j=json.load(open("'"$OUT/dig.json"'")); print(j.get("hub_pool") or (j.get("campaign") or {}).get("config",{}).get("hub_pool"))')"

# Report URLs + one-time customer token from response
REPORT_TOK=$(python3 -c 'import json; print(json.load(open("'"$OUT/dig.json"'")).get("customer_report_token") or "")')
python3 -c 'import json; j=json.load(open("'"$OUT/dig.json"'"));
print("report_url=", j.get("report_url")); print("gate_url=", j.get("gate_url")); print("pulse_url=", j.get("pulse_url")); print("report_token_len=", len(j.get("customer_report_token") or ""))' | tee -a "$OUT/run.log"
[[ -n "$REPORT_TOK" ]] || die "missing customer_report_token from Dig"

# 4) Wait for worker pickup — poll hub escrow + coordinator progress
RUNS=0
DEADLINE=$((SECONDS + WAIT_RUNS_SEC))
while (( SECONDS < DEADLINE )); do
  # hub escrow (local loopback on VPS not available — use public if allowed, else pool progress)
  PROG=$(curl -fsS --max-time 12 "${HUB}/pool/coordinator/api/fuzz/pool/campaigns/progress?id=${CID}" 2>/dev/null || true)
  if [[ -n "$PROG" ]]; then
    printf '%s' "$PROG" >"$OUT/progress.json"
    RUNS=$(python3 - <<'PY' "$OUT/progress.json" 2>/dev/null || echo 0
import json,sys
j=json.load(open(sys.argv[1]))
def pick(o):
  if not isinstance(o,dict): return 0
  for k in ("runs_done","runs","progress_runs"):
    if k in o:
      try: return int(o[k] or 0)
      except Exception: return 0
  return 0
n=pick(j) or pick(j.get("campaign") or {}) or pick(j.get("progress") or {})
print(n)
PY
)
  fi
  # also ask customer local campaign
  LOCAL_ARGS=(-fsS --max-time 8)
  if [[ -n "${ADMIN:-}" ]]; then
    LOCAL_ARGS+=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Authorization: Bearer $ADMIN")
  fi
  LOCAL=$(curl "${LOCAL_ARGS[@]}" "$BASE_LOCAL/api/fuzz/campaigns/$CID" 2>/dev/null || true)
  if [[ -n "$LOCAL" ]]; then
    printf '%s' "$LOCAL" >"$OUT/local_campaign.json"
    LR=$(python3 -c 'import json; j=json.load(open("'"$OUT/local_campaign.json"'")); s=j.get("summary") or {}; print(int(s.get("runs_done") or j.get("runs_done") or 0))' 2>/dev/null || echo 0)
    (( LR > RUNS )) && RUNS=$LR
  fi
  # hub prepaid drop / escrow via pool client cancel dry — use prepaid
  log "runs_done=$RUNS"
  (( RUNS >= 1 )) && break
  sleep 5
done

# 5) Cancel via customer local status API (must proxy hub_pool). No silent admin/hub-only mask.
log "cancel via local status API (customer path)"
CANCEL_ARGS=(-sS --max-time 90 -X POST -H 'Content-Type: application/json')
if [[ -n "${ADMIN:-}" ]]; then
  CANCEL_ARGS+=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Authorization: Bearer $ADMIN")
fi
curl "${CANCEL_ARGS[@]}" -d '{"status":"cancelled"}' \
  "$BASE_LOCAL/api/fuzz/campaigns/${CID}/status" >"$OUT/cancel_local.json" 2>"$OUT/cancel_local.err" \
  || die "local cancel HTTP failed ($(tail -c 300 "$OUT/cancel_local.err" 2>/dev/null || true))"
# Authoritative refund/escrow from customer pool client (same owner as Dig).
"$OUT/rc20_pool_client" cancel --campaign-id "$CID" >"$OUT/cancel_hub.json" 2>"$OUT/cancel_hub.err" \
  || die "hub cancel via pool client failed ($(tail -c 400 "$OUT/cancel_hub.err" 2>/dev/null || true) $(tail -c 400 "$OUT/cancel_hub.json" 2>/dev/null || true))"
PRE_AFTER=$(python3 -c 'import json
for p in ("'"$OUT/cancel_hub.json"'","'"$OUT/cancel_local.json"'"):
  try:
    j=json.load(open(p))
    if j.get("spendable_hmc") is not None:
      print(j["spendable_hmc"]); raise SystemExit
  except Exception: pass
print("")
')
ESC=$(python3 -c 'import json
for p in ("'"$OUT/cancel_hub.json"'","'"$OUT/cancel_local.json"'"):
  try:
    j=json.load(open(p)); e=j.get("escrow") or {}
    if e.get("status"): print(e["status"]); raise SystemExit
  except Exception: pass
print("")
')
log "prepaid_after_cancel=$PRE_AFTER escrow=$ESC"

# Report: local path must proxy hub; also probe hub directly with report token.
REPORT_HDR=()
if [[ -n "${REPORT_TOK:-}" ]]; then
  REPORT_HDR=(-H "X-Hackme-Report-Token: $REPORT_TOK")
fi
REPORT_CODE_LOCAL=$(curl -sS -o "$OUT/report_local.html" -w "%{http_code}" --max-time 30 \
  "${REPORT_HDR[@]}" "$BASE_LOCAL/api/fuzz/campaigns/${CID}/report.html" || echo err)
REPORT_CODE_HUB=$(curl -sS -o "$OUT/report_hub.html" -w "%{http_code}" --max-time 30 \
  "${REPORT_HDR[@]}" "$HUB/api/fuzz/campaigns/${CID}/report.html" || echo err)
log "report local=$REPORT_CODE_LOCAL hub=$REPORT_CODE_HUB"

python3 - <<PY >"$OUT/VERDICT.json"
import json
v={
  "ok": True,
  "stamp": "$STAMP",
  "pack": "$PACK",
  "version": "$VERSION",
  "commit": "$COMMIT",
  "owner": "$OWNER",
  "local_base": "$BASE_LOCAL",
  "fund_tx": "$TX",
  "campaign_id": "$CID",
  "runs_done": int("$RUNS" or 0),
  "hub_prepaid_after_fund": "$PRE_BEFORE",
  "hub_prepaid_after_cancel": "$PRE_AFTER",
  "escrow_status": "$ESC",
  "pickup": int("$RUNS" or 0) >= 1,
  "report_local_http": "$REPORT_CODE_LOCAL",
  "report_hub_http": "$REPORT_CODE_HUB",
  "prepaid_secret_shipped": False,
  "path": "build-orders-pack -> start local Orders -> Fund -> Dig(hub pool) -> cancel",
}
try:
  before=float("$PRE_BEFORE" or 0)
  after=float("$PRE_AFTER" or 0)
  budget=float("$BUDGET_HMC")
  v["refund_ok"]= after + 1e-6 >= (before - budget) and ("$ESC" in ("closed","") or after >= before - budget)
except Exception as e:
  v["refund_ok"]=False
  v["refund_err"]=str(e)
print(json.dumps(v, indent=2))
PY
cat "$OUT/VERDICT.json"
python3 - <<PY
import json,os,sys
v=json.load(open("$OUT/VERDICT.json"))
soft=os.environ.get("ALLOW_SOFT_PICKUP","0").strip()=="1"
if not v.get("refund_ok"):
  print("FAIL: refund path", flush=True); sys.exit(2)
loc=str(v.get("report_local_http") or "")
hub=str(v.get("report_hub_http") or "")
if not (loc.startswith("2") and hub.startswith("2")):
  print("FAIL: authenticated report must be 2xx on local proxy and hub (local=%s hub=%s)" % (loc, hub), flush=True)
  sys.exit(4)
if not v.get("pickup"):
  if soft:
    print("WARN: no runs — ALLOW_SOFT_PICKUP=1", flush=True)
  else:
    print("FAIL: no miner pickup in window (set ALLOW_SOFT_PICKUP=1 only for ops triage)", flush=True)
    sys.exit(3)
print("PASS demo money path", flush=True)
print("PACK=$PACK", flush=True)
print("OUT=$OUT", flush=True)
PY
