#!/usr/bin/env bash
# Real customer repo E2E: inventory → harness build → Hunt Standard pool → customer report.
#
# Default repo: DaveGamble/cJSON (upstream LLVMFuzzer harness in fuzzing/).
#
#   bash scripts/tests/hunt_customer_repo_e2e.sh
#   CJSON_REPO=/path/to/cjson bash scripts/tests/hunt_customer_repo_e2e.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd curl
require_cmd jq
require_cmd python3

if ! command -v clang >/dev/null 2>&1; then
  echo "[hunt-customer-e2e] SKIP: clang not installed" >&2
  exit 0
fi

export HACKME_REPO_ROOT="$ROOT"
CJSON_REPO="${CJSON_REPO:-$ROOT/.cache/oss-cve-clones/cjson}"
SOURCE_REL="${SOURCE_REL:-fuzzing/cjson_read_fuzzer.c}"
BUDGET_SHARDS="${BUDGET_SHARDS:-8}"
MIN_SHARDS_DONE="${MIN_SHARDS_DONE:-4}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-$ROOT/reports/hunt-customer-e2e/$STAMP}"
mkdir -p "$OUT"

log() { echo "[hunt-customer-e2e $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/run.log"; }

if [[ ! -f "$CJSON_REPO/$SOURCE_REL" ]]; then
  log "clone cJSON via build_oss_cve_pack"
  TARGETS=cjson bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$OUT/build-oss.log" 2>&1
fi
[[ -f "$CJSON_REPO/$SOURCE_REL" ]] || fail "missing harness $CJSON_REPO/$SOURCE_REL"

log "unit: inventory harness build (cJSON parent companion)"
go test -count=1 ./internal/hunt/ -run 'TestCollectParentCompanionsCjson|TestBuildInventoryHarnessCjsonCustomerRepo' -timeout=5m \
  >>"$OUT/go-test.log" 2>&1

COORD_DB="$(mktemp "${TMPDIR:-/tmp}/hackme-hunt-customer-e2e-coord.XXXXXX.db")"
rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true
COORD_PORT="${COORD_PORT:-$((19400 + RANDOM % 800))}"
COORD_ADDR="127.0.0.1:${COORD_PORT}"
COORD_BASE="http://${COORD_ADDR}"
ADMIN_TOKEN="${ADMIN_TOKEN:-hunt-customer-e2e-admin}"
WORKER_TOKEN="${WORKER_TOKEN:-hunt-customer-e2e-worker}"

NODE_PORT="${NODE_PORT:-$((19500 + RANDOM % 800))}"
NODE_ADDR="127.0.0.1:${NODE_PORT}"
NODE_BASE="http://${NODE_ADDR}"
NODE_DATA="$(mktemp -d "${TMPDIR:-/tmp}/hackme-hunt-customer-node.XXXXXX")"

COORD_PID=""
NODE_PID=""
cleanup() {
  [[ -n "$COORD_PID" ]] && kill "$COORD_PID" 2>/dev/null || true
  [[ -n "$NODE_PID" ]] && kill "$NODE_PID" 2>/dev/null || true
  rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true
  rm -rf "$NODE_DATA" 2>/dev/null || true
}
trap cleanup EXIT

log "start coordinator $COORD_BASE"
(
  export HACKME_COORDINATOR_DB="$COORD_DB"
  export HACKME_COORDINATOR_ADDR="$COORD_ADDR"
  export HACKME_COORDINATOR_ALLOW_INSECURE=1
  export HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN"
  export HACKME_COORDINATOR_WORKER_TOKEN="$WORKER_TOKEN"
  export HACKME_POOL_HYBRID_SIGNER_ENABLED=1
  export HACKME_POOL_HYBRID_SIGNER_STRICT=1
  go run ./cmd/coordinator
) >>"$OUT/coordinator.log" 2>&1 &
COORD_PID=$!

for _ in $(seq 1 50); do
  curl -fsS --max-time 2 "${COORD_BASE}/health" >/dev/null 2>&1 && break
  sleep 0.25
done
curl -fsS --max-time 3 "${COORD_BASE}/health" >/dev/null

NODE_BIN="${NODE_BIN:-$ROOT/bin/hackme-hunt-customer-e2e}"
log "build node → $NODE_BIN"
go build -trimpath -o "$NODE_BIN" .

log "start node $NODE_BASE"
nohup env \
  HACKME_DATA_DIR="$NODE_DATA" \
  HACKME_BIND_ADDR="$NODE_ADDR" \
  HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
  HACKME_FUZZ_AUTORUN=0 \
  HACKME_CHAIN_LEADER_LOCAL_POH=0 \
  HACKME_DESKTOP_MODE=1 \
  HACKME_POOL_COORDINATOR_URL="$COORD_BASE" \
  HACKME_COORDINATOR_URL="$COORD_BASE" \
  HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN" \
  HACKME_POOL_COORDINATOR_TOKEN="$ADMIN_TOKEN" \
  HACKME_POOL_SYNC_ASYNC=0 \
  "$NODE_BIN" >>"$OUT/node.log" 2>&1 &
NODE_PID=$!

for _ in $(seq 1 80); do
  if curl -fsS --max-time 2 "${NODE_BASE}/api/status?lite=1" >/dev/null 2>&1 \
    && curl -fsS --max-time 2 "${NODE_BASE}/api/hunt/packages" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$NODE_PID" 2>/dev/null; then
    tail -40 "$OUT/node.log" >&2 || true
    fail "node exited early"
  fi
  sleep 0.35
done
curl -fsS --max-time 10 -X POST "${NODE_BASE}/api/genesis" \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" -d '{}' >/dev/null

python3 - "$NODE_DATA" <<'PY'
import sqlite3, sys
data = sys.argv[1]
db = f"{data}/hackme.db"
units = 100 * 100_000_000  # 100 HMC for hunt_standard escrow
con = sqlite3.connect(db)
row = con.execute("SELECT address FROM wallet WHERE id=1").fetchone()
if not row:
    raise SystemExit("wallet row missing")
addr = row[0]
con.execute("UPDATE wallet SET balance_hmc=100, balance_units=?", (units,))
con.execute(
    "INSERT INTO accounts(address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now')) "
    "ON CONFLICT(address) DO UPDATE SET balance_units=excluded.balance_units",
    (addr, units),
)
con.commit()
PY
log "funded operator wallet (100 HMC) for hunt_standard escrow"

admin_hdr=(-H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" -H "Content-Type: application/json")

log "1/4 inventory scan $CJSON_REPO"
INV="$(curl_retry_fsS -fsS -X POST "${NODE_BASE}/api/hunt/inventory" \
  "${admin_hdr[@]}" \
  -d "$(jq -nc --arg p "$CJSON_REPO" '{path:$p, max_files:400}')")"
echo "$INV" | jq . >"$OUT/inventory.json"
echo "$INV" | jq -e --arg rel "$SOURCE_REL" '
  .ok == true
  and (.inventory.targets | map(.path) | index($rel) != null)
' >/dev/null || fail "inventory missing $SOURCE_REL"

log "2/4 harness build $SOURCE_REL"
BUILD="$(curl_retry_fsS -fsS -X POST "${NODE_BASE}/api/hunt/harness/build" \
  "${admin_hdr[@]}" \
  -d "$(jq -nc --arg p "$CJSON_REPO" --arg s "$SOURCE_REL" \
    '{repo:{path:$p, git_url:"https://github.com/DaveGamble/cJSON", ref:"master"}, source_rel:$s, template_accept:false}')")"
echo "$BUILD" | jq . >"$OUT/harness-build.json"
HARNESS_HASH="$(echo "$BUILD" | jq -r '.build.harness_hash // .harness_hash // empty')"
[[ -n "$HARNESS_HASH" && "$HARNESS_HASH" != "null" ]] || fail "harness build missing hash"
echo "$BUILD" | jq -e '.build.companion_sources | map(select(test("cJSON\\.c$"))) | length >= 1' >/dev/null \
  || fail "expected cJSON.c companion in build"

CID="hunt-customer-cjson-${STAMP}"
log "3/4 create Hunt Standard pool campaign $CID (shards=$BUDGET_SHARDS)"
CREATE="$(curl_retry_fsS -fsS -X POST "${NODE_BASE}/api/hunt/campaigns" \
  "${admin_hdr[@]}" \
  -d "$(jq -nc \
    --arg id "$CID" \
    --arg p "$CJSON_REPO" \
    --arg rel "$SOURCE_REL" \
    --argjson shards "$BUDGET_SHARDS" \
    '{
      id: $id,
      package: "hunt_standard",
      title: "Customer E2E · cJSON",
      pool_distributed: true,
      budget_shards: $shards,
      status: "running",
      catalog: false,
      repo: {path: $p, git_url: "https://github.com/DaveGamble/cJSON", ref: "master"},
      inventory_target: {path: $rel, title: "cJSON read fuzzer", source: "inventory"},
      template_accept: false
    }')")"
echo "$CREATE" | jq . >"$OUT/create.json"
CID="$(echo "$CREATE" | jq -r '.campaign.id // empty')"
[[ -n "$CID" ]] || fail "missing campaign id"
REPORT_TOKEN="$(echo "$CREATE" | jq -r '.customer_report_token // empty')"
[[ -n "$REPORT_TOKEN" ]] || fail "missing customer_report_token"
echo "$CREATE" | jq -e '.ok == true and (.pool_sync == "ok" or .pool_sync == "queued" or .pool_sync == "sync")' >/dev/null \
  || fail "pool_sync failed: $(echo "$CREATE" | jq -c '{pool_sync,pool_sync_warning}')"

# Wait until coordinator lists the campaign.
for _ in $(seq 1 40); do
  if curl -fsS --max-time 5 "${COORD_BASE}/api/fuzz/pool/campaigns/progress?id=${CID}" \
    | jq -e '.ok == true' >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

WORKERFUZZ_BIN="${WORKERFUZZ_BIN:-$ROOT/bin/workerfuzz-hunt-customer-e2e}"
log "run workerfuzz (min $MIN_SHARDS_DONE shards)"
go build -trimpath -o "$WORKERFUZZ_BIN" ./cmd/workerfuzz
WORKERFUZZ_TIMEOUT_MS="${WORKERFUZZ_TIMEOUT_MS:-120000}"
set +e
timeout 150s "$WORKERFUZZ_BIN" \
  -coord "$COORD_BASE" \
  -token "$WORKER_TOKEN" \
  -worker "hunt-customer-w1" \
  -timeout-ms "$WORKERFUZZ_TIMEOUT_MS" >>"$OUT/workerfuzz.log" 2>&1
set -e

COORD_DONE="$(python3 - "$COORD_DB" "$CID" <<'PY'
import sqlite3, sys
db, cid = sys.argv[1], sys.argv[2]
con = sqlite3.connect(db)
n = con.execute(
    "SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='done' AND result_ok=1",
    (cid,)).fetchone()[0]
print(n)
if n < 1:
    raise SystemExit(f"coordinator has no done shards for {cid}")
PY
)"
[[ "${COORD_DONE:-0}" -ge "$MIN_SHARDS_DONE" ]] \
  || fail "coordinator shards_done=$COORD_DONE want >=$MIN_SHARDS_DONE"
log "coordinator verified shards_done=$COORD_DONE"

# Best-effort mirror into node summary for customer report.
NODE_DONE=0
for _ in $(seq 1 8); do
  curl -fsS --max-time 10 "${NODE_BASE}/api/fuzz/marketplace" >/dev/null 2>&1 || true
  NODE_DONE="$(curl -fsS --max-time 8 "${NODE_BASE}/api/fuzz/campaigns/${CID}" \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" | jq -r '.campaign.summary.runs_done // 0' 2>/dev/null || echo 0)"
  if [[ "${NODE_DONE:-0}" -ge "$MIN_SHARDS_DONE" ]]; then
    break
  fi
  sleep 2
done
if [[ "${NODE_DONE:-0}" -lt "$MIN_SHARDS_DONE" ]]; then
  log "warn: node runs_done=${NODE_DONE:-0} (coordinator=$COORD_DONE) — report may lag pool sync"
fi

log "4/4 customer report (token-gated JSON + HTML)"
REPORT_JSON="$(curl_retry_fsS -fsS \
  "${NODE_BASE}/api/fuzz/campaigns/${CID}/report?format=json&limit=50" \
  -H "X-Hackme-Report-Token: ${REPORT_TOKEN}")"
echo "$REPORT_JSON" | jq . >"$OUT/report.json"

curl_retry_fsS -fsS \
  "${NODE_BASE}/api/fuzz/campaigns/${CID}/report.html" \
  -H "X-Hackme-Report-Token: ${REPORT_TOKEN}" \
  >"$OUT/report.html"

python3 - "$OUT/report.json" "$MIN_SHARDS_DONE" "$COORD_DONE" <<'PY'
import json, sys
path, min_done, coord_done = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
r = json.load(open(path))
assert r.get("ok") is True, r
assert coord_done >= min_done, f"coordinator shards_done={coord_done}"
camp = r.get("campaign") or {}
cfg = camp.get("config") or {}
summary = camp.get("summary") or {}
depth = summary.get("hunt_depth") or {}
iter_ps = depth.get("iterations_per_shard") or cfg.get("iterations_per_shard")
assert iter_ps == 128, f"iterations_per_shard={iter_ps}"
assert cfg.get("hunt_source") == "inventory", cfg.get("hunt_source")
assert cfg.get("hunt_package") == "hunt_standard", cfg.get("hunt_package")
runs = int((r.get("gate") or {}).get("observed", {}).get("runs_done") or summary.get("runs_done") or 0)
verdict = r.get("verdict", "")
assert verdict in ("clean", "warn_medium", "warn_crash"), verdict
print(json.dumps({
    "verdict": verdict,
    "coordinator_shards_done": coord_done,
    "node_runs_done": runs,
    "iterations_per_shard": iter_ps,
    "hunt_source": cfg.get("hunt_source"),
    "hunt_package": cfg.get("hunt_package"),
    "human_summary": r.get("human_summary"),
}, indent=2))
PY

jq -n \
  --arg stamp "$STAMP" \
  --arg cid "$CID" \
  --arg repo "$CJSON_REPO" \
  --arg source "$SOURCE_REL" \
  --arg harness "$HARNESS_HASH" \
  --arg coord_done "$COORD_DONE" \
  --arg node_done "${NODE_DONE:-0}" \
  '{stamp:$stamp, campaign_id:$cid, repo:$repo, source_rel:$source, harness_hash:$harness,
    coordinator_runs_done:($coord_done|tonumber), node_runs_done:($node_done|tonumber),
    package:"hunt_standard", iterations_per_shard:128, flow:"inventory→build→pool→report"}' \
  >"$OUT/summary.json"

log "PASS → $OUT (coordinator_done=$COORD_DONE node_done=${NODE_DONE:-0})"
pass "hunt_customer_repo_e2e PASS ($OUT)"
