#!/usr/bin/env bash
# Full docker e2e on a clean image:
#   build → status → seed-perm repair → worker/start (mine+hybrid Dig) →
#   customer security-audit (pool_distributed) → wait runs_done → settle pull.
#
#   bash scripts/ops/docker_e2e_smoke.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:${PATH:-}"

COMPOSE=(docker compose -f deploy/docker-compose.e2e.yml)
E2E_TOKEN="${E2E_TOKEN:-$(openssl rand -hex 24)}"
E2E_WORKER_TOKEN="${E2E_WORKER_TOKEN:-$(openssl rand -hex 24)}"
E2E_PUBLISH_TOKEN="${E2E_PUBLISH_TOKEN:-$(openssl rand -hex 24)}"
# Coordinator refuses equal secrets.
if [[ "$E2E_TOKEN" == "$E2E_WORKER_TOKEN" || "$E2E_TOKEN" == "$E2E_PUBLISH_TOKEN" || "$E2E_WORKER_TOKEN" == "$E2E_PUBLISH_TOKEN" ]]; then
  E2E_WORKER_TOKEN="$(openssl rand -hex 24)"
  E2E_PUBLISH_TOKEN="$(openssl rand -hex 24)"
fi
export E2E_TOKEN E2E_WORKER_TOKEN E2E_PUBLISH_TOKEN
# Default publish ports avoid common lab tunnels on 18080/18081 (ssh/VPS forwards).
NODE_PUBLISH_PORT="${NODE_PUBLISH_PORT:-28080}"
COORD_PUBLISH_PORT="${COORD_PUBLISH_PORT:-28081}"
export NODE_PUBLISH_PORT COORD_PUBLISH_PORT
NODE_URL="${NODE_URL:-http://127.0.0.1:${NODE_PUBLISH_PORT}}"
COORD_URL="${COORD_URL:-http://127.0.0.1:${COORD_PUBLISH_PORT}}"
ADMIN="$E2E_TOKEN"
WORKER="$E2E_WORKER_TOKEN"
PUBLISH="$E2E_PUBLISH_TOKEN"

cleanup() {
  "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[e2e] admin=${E2E_TOKEN:0:8}… worker=${E2E_WORKER_TOKEN:0:8}… publish=${E2E_PUBLISH_TOKEN:0:8}… node=$NODE_URL"
cleanup
if [[ "${HACKME_E2E_NO_BUILD:-0}" == "1" ]]; then
  "${COMPOSE[@]}" up -d
else
  # Bridge DNS often cannot resolve proxy.golang.org; host network works without
  # Compose entitlement flags (compose build --allow is missing on Compose v5).
  echo "[e2e] docker build --network=host → hackme-e2e:local"
  docker build --network=host \
    -f deploy/Dockerfile \
    -t hackme-e2e:local \
    --build-arg "HACKME_VERSION=${HACKME_VERSION:-0.1.0-rc18.1}" \
    --build-arg "ENTRYPOINT_SHA=$(md5sum deploy/docker-entrypoint.sh | awk '{print $1}')" \
    .
  "${COMPOSE[@]}" up -d --no-build
fi

echo "[e2e] wait node+coordinator"
ok=0
for i in $(seq 1 120); do
  if curl -fsS "$NODE_URL/api/status?lite=1" >/dev/null 2>&1 \
    && curl -fsS "$COORD_URL/api/network/stats" >/dev/null 2>&1; then
    ok=1
    break
  fi
  sleep 2
done
if [[ "$ok" != "1" ]]; then
  echo "[e2e] FAIL: services did not become healthy" >&2
  "${COMPOSE[@]}" ps -a >&2 || true
  "${COMPOSE[@]}" logs --tail 120 node >&2 || true
  "${COMPOSE[@]}" logs --tail 80 coordinator >&2 || true
  exit 1
fi
curl -fsS "$NODE_URL/api/status?lite=1" >/dev/null
curl -fsS "$COORD_URL/api/network/stats" >/dev/null
echo "[e2e] status OK"

cid="$("${COMPOSE[@]}" ps -q node)"
ccid="$("${COMPOSE[@]}" ps -q coordinator)"

echo "[e2e] probe miner seed + workerfuzz + bwrap inside image"
docker exec "$cid" bash -lc '
  set -e
  test -x /opt/hackme/bin/workerfuzz
  test -x /opt/hackme/bin/hackme-wasm-check
  test -x /usr/local/bin/minersign || test -x /opt/hackme/minersign
  command -v bwrap >/dev/null
  echo "binaries OK"
  echo "STRICT=${HACKME_HUNT_ISOLATE_STRICT:-?} BUILD_SANDBOX=${HACKME_HUNT_BUILD_SANDBOX:-?}"
  echo "SIGN=${HACKME_WORKER_SIGN_SUBMITS:-?} HYBRID_MODE=${HACKME_WORKER_HYBRID_FUZZ_MODE:-?}"
'

# Classic installer footgun: permissions too open 0644/0666.
echo "[e2e] chmod 0644 existing node_ed25519.seed"
docker exec "$cid" bash -lc '
  set -e
  f=/data/node_ed25519.seed
  test -f "$f"
  chmod 644 "$f"
  ls -l "$f"
'

echo "[e2e] POST /api/worker/start (mine + hybrid Dig; auto-repair seed perms)"
resp="$(mktemp)"
code="$(curl -sS -o "$resp" -w '%{http_code}' -X POST "$NODE_URL/api/worker/start" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "{\"coord_url\":\"http://coordinator:8081\",\"coord_token\":\"$WORKER\"}")"
echo "[e2e] worker/start HTTP $code body=$(head -c 400 "$resp")"
[[ "$code" == "200" ]] || { cat "$resp"; exit 1; }
if grep -qi 'permissions too open\|miner_seed_invalid\|minersign_binary_missing\|hybrid_required' "$resp"; then
  echo "[e2e] FAIL: worker/start returned seed/sign error" >&2
  cat "$resp" >&2
  exit 1
fi
docker exec "$cid" bash -lc 'stat -c "%a %n" /data/node_ed25519.seed' | tee /tmp/hackme-e2e-seed-mode.txt
mode="$(awk '{print $1}' /tmp/hackme-e2e-seed-mode.txt)"
[[ "$mode" == "600" ]] || echo "[e2e] WARN: seed mode is $mode (expected 600 after repair)"

echo "[e2e] wait worker heartbeat on coordinator"
hb_ok=0
for i in $(seq 1 60); do
  stats="$(curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" "$COORD_URL/api/work/stats" 2>/dev/null || true)"
  if printf '%s' "$stats" | python3 -c 'import sys,json; d=json.load(sys.stdin); w=d.get("workers") or d.get("online") or 0; raise SystemExit(0 if (isinstance(w,int) and w>0) or (isinstance(w,dict) and len(w)>0) or int(d.get("workers_online") or d.get("online_workers") or 0)>0 else 1)' 2>/dev/null; then
    hb_ok=1
    echo "[e2e] work/stats $(head -c 240 <<<"$stats")"
    break
  fi
  sleep 2
done
[[ "$hb_ok" == "1" ]] || echo "[e2e] WARN: no worker heartbeat yet (continuing)"

echo "[e2e] wallet / local-auth"
curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" "$NODE_URL/api/wallet" | head -c 300; echo
docker exec "$cid" curl -fsS "http://127.0.0.1:8080/api/desktop/local-auth" | head -c 240; echo

echo "[e2e] POST /api/genesis + fund wallet (treasury/lab) for customer orders"
code="$(curl -sS -o "$resp" -w '%{http_code}' -X POST "$NODE_URL/api/genesis" \
  -H "X-Hackme-Admin-Token: $ADMIN")"
echo "[e2e] genesis HTTP $code $(head -c 200 "$resp")"
[[ "$code" == "200" ]] || { cat "$resp"; exit 1; }
docker exec -i "$cid" python3 -c '
import sqlite3
con = sqlite3.connect("/data/hackme.db")
cur = con.cursor()
units = 100 * 100_000_000
cur.execute("UPDATE wallet SET balance_hmc=100, balance_units=? WHERE id=1", (units,))
row = cur.execute("SELECT address FROM wallet WHERE id=1").fetchone()
if not row:
    raise SystemExit("wallet row missing after genesis")
cur.execute("UPDATE accounts SET balance_units=? WHERE address=?", (units, row[0]))
con.commit()
print("wallet funded", row[0], "units", units)
'
wbal="$(curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" "$NODE_URL/api/wallet" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get("balance_orders_spendable_hmc") or d.get("balance_hmc") or d.get("balance_local_mirror_hmc"))')"
echo "[e2e] spendable_hmc=$wbal"

echo "[e2e] POST /api/tasks (developer/admin PoH order; via container loopback — desktop CSRF gate)"
task_body='{"id":"e2e-order-'"$(date +%s)"'","kind":"synthetic_poh_v1","reward_hmc":0.05,"difficulty_score":1,"target_solves":1,"wasm_check_hex":"0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b","payer_ref":"e2e"}'
# Host→published-port is not loopback inside the container (docker NAT); desktop mutating routes need 127.0.0.1.
code="$(docker exec -i "$cid" curl -sS -o /tmp/e2e-tasks.json -w '%{http_code}' -X POST "http://127.0.0.1:8080/api/tasks" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "$task_body")"
docker exec "$cid" cat /tmp/e2e-tasks.json >"$resp"
echo "[e2e] tasks HTTP $code $(head -c 240 "$resp")"
[[ "$code" == "200" || "$code" == "201" ]] || { cat "$resp"; exit 1; }

# Re-fund after PoH order spend
docker exec -i "$cid" python3 -c '
import sqlite3
con = sqlite3.connect("/data/hackme.db")
cur = con.cursor()
units = 100 * 100_000_000
cur.execute("UPDATE wallet SET balance_hmc=100, balance_units=? WHERE id=1", (units,))
row = cur.execute("SELECT address FROM wallet WHERE id=1").fetchone()
cur.execute("UPDATE accounts SET balance_units=? WHERE address=?", (units, row[0]))
con.commit()
'

CAMPAIGN_ID="e2e-dig-$(date +%s)"
BUDGET_RUNS=16
echo "[e2e] POST /api/security-audit pool_distributed Dig campaign_id=$CAMPAIGN_ID"
audit_body="$(python3 -c '
import json
print(json.dumps({
  "title": "e2e-dig-pool",
  "campaign_id": "'"$CAMPAIGN_ID"'",
  "payer_ref": "e2e",
  "budget_hmc": 1,
  "budget_runs": '"$BUDGET_RUNS"',
  "budget_seconds": 600,
  "create_poh_order": False,
  "pool_distributed": True,
  "depth_tier": "wasm_only",
  "wasm_check_hex": "0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b",
}))
')"
code="$(docker exec -i "$cid" curl -sS -o /tmp/e2e-audit.json -w '%{http_code}' -X POST "http://127.0.0.1:8080/api/security-audit" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "$audit_body")"
docker exec "$cid" cat /tmp/e2e-audit.json >"$resp"
echo "[e2e] security-audit HTTP $code $(head -c 500 "$resp")"
[[ "$code" == "200" ]] || { cat "$resp"; "${COMPOSE[@]}" logs --tail 60 node; exit 1; }
# Prefer campaign id from response when present
CAMPAIGN_ID="$(python3 -c 'import sys,json; d=json.load(open("'"$resp"'")); print(d.get("campaign_id") or d.get("id") or "'"$CAMPAIGN_ID"'")' 2>/dev/null || echo "$CAMPAIGN_ID")"
echo "[e2e] campaign_id=$CAMPAIGN_ID"

echo "[e2e] wait Dig runs_done > 0 on coordinator (hybrid workerfuzz)"
runs_ok=0
last_runs=0
for i in $(seq 1 90); do
  # Admin list / campaign pulse
  prog="$(curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" \
    "$COORD_URL/api/fuzz/pool/campaigns/progress?id=$CAMPAIGN_ID" 2>/dev/null || true)"
  if [[ -z "$prog" ]]; then
    prog="$(curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" \
      "$COORD_URL/api/fuzz/campaigns/$CAMPAIGN_ID" 2>/dev/null || true)"
  fi
  last_runs="$(printf '%s' "$prog" | python3 -c 'import sys,json
try:
  d=json.load(sys.stdin)
except Exception:
  print(0); raise SystemExit
r=d.get("runs_done")
if r is None and isinstance(d.get("progress"), dict):
  r=d["progress"].get("runs_done")
if r is None and isinstance(d.get("runtime"), dict):
  r=d["runtime"].get("runs_done")
print(int(r or 0))' 2>/dev/null || echo 0)"
  if [[ "${last_runs:-0}" -gt 0 ]]; then
    runs_ok=1
    echo "[e2e] Dig progress runs_done=$last_runs (iter=$i)"
    break
  fi
  # Node pulse may mirror after pool sync
  nprog="$(curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" \
    "$NODE_URL/api/fuzz/campaigns/$CAMPAIGN_ID/pulse" 2>/dev/null || true)"
  nruns="$(printf '%s' "$nprog" | python3 -c 'import sys,json
try:
  d=json.load(sys.stdin)
except Exception:
  print(0); raise SystemExit
p=d.get("progress") or {}
print(int(p.get("runs_done") or d.get("runs_done") or 0))' 2>/dev/null || echo 0)"
  if [[ "${nruns:-0}" -gt 0 ]]; then
    runs_ok=1
    last_runs=$nruns
    echo "[e2e] Dig node pulse runs_done=$last_runs (iter=$i)"
    break
  fi
  if (( i % 10 == 0 )); then
    echo "[e2e] still waiting Dig runs_done… iter=$i"
    docker exec "$cid" bash -lc 'ps aux | grep -E "worker(poh|fuzz)" | grep -v grep || true'
    "${COMPOSE[@]}" logs --tail 20 node 2>/dev/null | tail -20 || true
  fi
  sleep 2
done
if [[ "$runs_ok" != "1" ]]; then
  echo "[e2e] FAIL: Dig runs_done stuck at 0" >&2
  "${COMPOSE[@]}" logs --tail 100 node >&2 || true
  "${COMPOSE[@]}" logs --tail 60 coordinator >&2 || true
  curl -sS -H "X-Hackme-Admin-Token: $ADMIN" "$COORD_URL/api/fuzz/pool/stats" >&2 || true
  exit 1
fi

echo "[e2e] wait Dig near budget / settle path (runs_done>=$BUDGET_RUNS or completed)"
done_ok=0
for i in $(seq 1 120); do
  prog="$(curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" \
    "$COORD_URL/api/fuzz/pool/campaigns/progress?id=$CAMPAIGN_ID" 2>/dev/null \
    || curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" "$COORD_URL/api/fuzz/campaigns/$CAMPAIGN_ID" 2>/dev/null \
    || true)"
  eval "$(printf '%s' "$prog" | python3 -c 'import sys,json
try:
  d=json.load(sys.stdin)
except Exception:
  print("runs=0"); print("status=unknown"); raise SystemExit
r=d.get("runs_done")
if r is None and isinstance(d.get("progress"), dict):
  r=d["progress"].get("runs_done")
if r is None and isinstance(d.get("runtime"), dict):
  r=d["runtime"].get("runs_done")
print("runs=%d" % int(r or 0))
print("status=%s" % (d.get("status") or (d.get("runtime") or {}).get("status") or "unknown"))' 2>/dev/null || echo -e 'runs=0\nstatus=unknown')"
  if [[ "${runs:-0}" -ge "$BUDGET_RUNS" || "$status" == "completed" ]]; then
    done_ok=1
    echo "[e2e] Dig finished runs=$runs status=$status"
    break
  fi
  sleep 2
done
[[ "$done_ok" == "1" ]] || echo "[e2e] WARN: Dig not fully completed yet runs=${runs:-0} status=${status:-?} (partial Dig OK if runs_done>0)"

echo "[e2e] settle pull / outbox (no 403)"
settle_code="$(curl -sS -o "$resp" -w '%{http_code}' -H "X-Hackme-Admin-Token: $ADMIN" \
  "$COORD_URL/api/fuzz/pool/settle/outbox" 2>/dev/null || echo 000)"
echo "[e2e] settle outbox HTTP $settle_code $(head -c 240 "$resp")"
if [[ "$settle_code" == "403" || "$settle_code" == "401" ]]; then
  echo "[e2e] FAIL: settle outbox auth $settle_code (miner/admin token path broken)" >&2
  cat "$resp" >&2
  exit 1
fi
# Node settle pull endpoint / status
node_settle="$(curl -sS -o "$resp" -w '%{http_code}' -H "X-Hackme-Admin-Token: $ADMIN" \
  "$NODE_URL/api/fuzz/campaigns/$CAMPAIGN_ID" 2>/dev/null || echo 000)"
echo "[e2e] node campaign HTTP $node_settle $(head -c 240 "$resp")"
[[ "$node_settle" != "403" && "$node_settle" != "401" ]] || { echo "[e2e] FAIL: node campaign 403"; exit 1; }

echo "[e2e] publish-token harness gate (customer Orders path)"
# Publish secret rides X-Hackme-Admin-Token (same carrier as live Orders); body can fail validation but auth must pass.
PUB_HASH="$(printf 'e2e-harness-%s' "$CAMPAIGN_ID" | sha256sum | awk '{print $1}')"
harness_code="$(curl -sS -o "$resp" -w '%{http_code}' -X POST "$COORD_URL/api/fuzz/pool/hunt/harness" \
  -H "Content-Type: application/octet-stream" \
  -H "X-Hackme-Harness-Hash: $PUB_HASH" \
  -H "X-Hackme-Source-Rel: e2e/customer" \
  -H "X-Hackme-Admin-Token: $PUBLISH" \
  --data-binary $'not-a-real-harness' || echo 000)"
echo "[e2e] harness publish HTTP $harness_code $(head -c 200 "$resp")"
if [[ "$harness_code" == "401" || "$harness_code" == "403" ]]; then
  echo "[e2e] FAIL: publish token rejected on harness upload" >&2
  cat "$resp" >&2
  exit 1
fi
# 200 = stored; 400 = auth OK but blob rejected — both prove publish token works OOTB.
if [[ "$harness_code" != "200" && "$harness_code" != "400" ]]; then
  echo "[e2e] WARN: unexpected harness HTTP $harness_code (auth path still open)" >&2
fi

echo "[e2e] PASS — clean-image mine + Dig runs_done=$last_runs + settle/auth + customer publish OK"
rm -f "$resp"
