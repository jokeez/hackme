#!/usr/bin/env bash
# Fair pool order competition: VPS pool-only orders + multi local workers + miner audit.
# Usage:
#   NODE_SSH=hackme-vps bash scripts/ops/apply_miner_fair_pool.sh
#   bash scripts/ops/fair_pool_order_compete_test.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
NODE_SSH="${NODE_SSH:-hackme-vps}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
LOCAL_WORKERS="${LOCAL_WORKERS:-3}"
REWARD="${REWARD_HMC:-0.5}"
TARGET="${TARGET_SOLVES:-4}"
PREPAID="$(python3 -c "print(${REWARD}*${TARGET})")"

POOL_TOKEN="${POOL_TOKEN:-$(cat "$ROOT/dist/release_0.1.0-rc11i/linux/pool.miner.token" 2>/dev/null || true)}"
if [[ -z "$POOL_TOKEN" ]]; then
  echo "[fair-test] set POOL_TOKEN or build dist release with pool.miner.token" >&2
  exit 1
fi

go build -o "$ROOT/bin/workerpoh" ./cmd/workerpoh 2>/dev/null || true
go build -o "$ROOT/bin/workerpoh-cuda" -tags cuda ./cmd/workerpoh 2>/dev/null || true
WORKER_BIN="${WORKER_BIN:-$ROOT/bin/workerpoh-cuda}"
[[ -x "$WORKER_BIN" ]] || WORKER_BIN="$ROOT/bin/workerpoh"

log() { echo "[fair-test] $*"; }

log "apply fair pool on $NODE_SSH (idempotent)"
NODE_SSH="$NODE_SSH" bash "$ROOT/scripts/ops/apply_miner_fair_pool.sh" 2>&1 | tail -25

log "stop old local workers"
pkill -f 'workerpoh.*worker-kapa-pc' 2>/dev/null || true
sleep 1

mkdir -p "$ROOT/logs/fair-pool"
SEED_BASE="${HACKME_FAIR_TEST_SEED_BASE:-}"
if [[ -z "$SEED_BASE" ]]; then
  SEED_BASE="$(openssl rand -hex 32)"
fi

log "start $LOCAL_WORKERS local pool workers (unique hybrid addresses)"
for i in $(seq 1 "$LOCAL_WORKERS"); do
  wid="worker-kapa-fair-${i}"
  seed="$(printf '%s' "$SEED_BASE" | python3 -c "import hashlib,sys; s=sys.stdin.read().strip(); print(hashlib.sha256(f'{s}:fair:{sys.argv[1]}'.encode()).hexdigest())" "$i")"
  nf="$ROOT/logs/fair-pool/nonce-${wid}.txt"
  echo 0 >"$nf"
  pub="$(HACKME_MINER_ED25519_SEED_HEX="$seed" go run ./cmd/minersign -show-address 2>/dev/null)" || pub="?"
  log "  $wid -> $pub"
  HACKME_MINER_ED25519_SEED_HEX="$seed" \
  HACKME_WORKER_SIGN_SUBMITS=1 \
  HACKME_DESKTOP_GPU_POOL=1 \
  HACKME_GPU_FLEET=0 \
  BATCH_SIZE=1048576 \
  GPU_CHUNK=1048576 \
  nohup "$WORKER_BIN" \
    -coord "$COORD_URL" \
    -token "$POOL_TOKEN" \
    -worker "$wid" \
    -batch 1048576 \
    -gpu-chunk 1048576 \
    -search-timeout-ms 3000 \
    -gpu-backend cuda \
    >"$ROOT/logs/fair-pool/${wid}.log" 2>&1 &
done
sleep 3

log "create order on VPS (${TARGET}×${REWARD} HMC prepaid=${PREPAID})"
ORDER_ID="order-fair-all-$(date +%s)"
export ORDER_ID REWARD TARGET
ssh -o BatchMode=yes "$NODE_SSH" 'source /opt/hackme/.env.vps
WASM=0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b
BODY=$(python3 -c "
import json,os
print(json.dumps({
  \"id\": os.environ[\"ORDER_ID\"],
  \"kind\": \"synthetic_poh_v1\",
  \"reward_hmc\": float(os.environ[\"REWARD\"]),
  \"target_solves\": int(os.environ[\"TARGET\"]),
  \"difficulty_score\": 1,
  \"payer_ref\": \"fair-pool:all-miners\",
  \"wasm_check_hex\": \"0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b\",
}))
")
curl -fsS -X POST http://127.0.0.1:18080/api/tasks \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
  --data-binary "$BODY" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get(\"ok\"),d.get(\"id\"),d.get(\"total_debit_hmc\"))"'
echo "$ORDER_ID" >"$ROOT/logs/fair-pool/last_order_id.txt"

log "poll progress up to 3 min"
for n in $(seq 1 18); do
  read -r st prog tgt <<<"$(
    ssh -o BatchMode=yes "$NODE_SSH" "source /opt/hackme/.env.vps
    curl -fsS http://127.0.0.1:18080/api/tasks -H \"X-Hackme-Admin-Token: \${HACKME_ADMIN_TOKEN}\" | python3 -c \"
import json,sys,os
oid=os.environ.get('OID','')
for t in json.load(sys.stdin).get('tasks',[]):
  if t.get('id')==oid:
    print(t.get('status'),t.get('progress_count'),t.get('target_solves'))
    break
\"" OID="$ORDER_ID" 2>/dev/null || echo "open 0 $TARGET"
  )"
  log "  t=${n} status=$st progress=$prog/$tgt"
  [[ "$st" == "completed" ]] && break
  sleep 10
done

log "miner distribution (order blocks)"
ssh -o BatchMode=yes "$NODE_SSH" "python3 - <<'PY'
import sqlite3,json,base64,os
oid=os.environ['OID']
db=sqlite3.connect('/opt/hackme/data/hackme.db')
by={}
for idx,raw in db.execute('SELECT block_index, json FROM blocks ORDER BY block_index DESC LIMIT 400'):
  try: j=json.loads(raw)
  except: continue
  pl=(j.get('task') or {}).get('payload','')
  found=''
  if isinstance(pl,str):
    for fn in (lambda x: json.loads(base64.b64decode(x)), lambda x: json.loads(x)):
      try:
        p=fn(pl); found=(p.get('order_task_id') or '')
        if found: break
      except: pass
  if found==oid:
    m=j.get('miner_address','?')
    by[m]=by.get(m,0)+1
row=db.execute('SELECT status,progress_count,target_solves FROM tasks WHERE id=?',(oid,)).fetchone()
print('task',row)
print('miners',by)
print('pool_only_leader', 'HMC-381c0c5e2cfcc714' in by and len(by)==1)
PY" OID="$ORDER_ID"

log "coordinator claim sample"
curl -fsS -X POST "$COORD_URL/api/work/claim" \
  -H "Authorization: Bearer $POOL_TOKEN" \
  -H "Content-Type: application/json" \
  --data '{"worker_id":"worker-kapa-fair-1","batch_size":524288}' \
  | python3 -c "import json,sys; d=json.load(sys.stdin); print('scheduler',d.get('scheduler_mode'),'order',d.get('order_task_id'),'wasm',len(d.get('wasm_check_hex') or ''))" || true

log "done — logs: logs/fair-pool/*.log"
