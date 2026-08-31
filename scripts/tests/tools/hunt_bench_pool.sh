#!/usr/bin/env bash
# Pool shard benchmark: Hunt Standard iterations_per_shard on coordinator.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

TARGET_ID="${TARGET_ID:-cjson}"
POOL_SHARDS="${POOL_SHARDS:-4}"
POOL_ITER_PER_SHARD="${POOL_ITER_PER_SHARD:-128}"
OUT_JSON="${OUT_JSON:-/tmp/hunt-pool-bench.json}"

COORD_DB="${COORD_DB:-$(mktemp "${TMPDIR:-/tmp}/hackme-hunt-bench-pool.XXXXXX.db")}"
rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true
export HACKME_COORDINATOR_DB="$COORD_DB"
COORD_PORT="${COORD_PORT:-$((19200 + RANDOM % 800))}"
export HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}"
BASE="http://${HACKME_COORDINATOR_ADDR}"
export HACKME_COORDINATOR_ALLOW_INSECURE=1
export HACKME_COORDINATOR_ADMIN_TOKEN=""
export HACKME_COORDINATOR_WORKER_TOKEN="hunt-bench-worker"
export HACKME_POOL_HYBRID_SIGNER_ENABLED=1
export HACKME_POOL_HYBRID_SIGNER_STRICT=1

HARNESS_HASH="$(python3 - "$ROOT" "$TARGET_ID" <<'PY'
import hashlib, json, os, sys
root, tid = sys.argv[1], sys.argv[2]
with open(os.path.join(root, "upstream", "oss_cve_targets.json")) as f:
    data = json.load(f)
t = next(x for x in data["targets"] if x["id"] == tid)
parts = t["id"] + t["repo"] + t["ref"] + t["driver"]
parts += ",".join(t.get("upstream_src", [])) + ",".join(t.get("build_flags", []))
print(hashlib.sha256(parts.encode()).hexdigest()[:32])
PY
)"

go run ./cmd/coordinator &
CPID=$!
cleanup() {
  kill "$CPID" 2>/dev/null || true
  rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true
}
trap cleanup EXIT

for _ in $(seq 1 50); do
  curl -fsS --max-time 2 "${BASE}/health" >/dev/null 2>&1 && break
  sleep 0.2
done

MAIN_CID="hunt-bench-$(date +%s)"
curl -fsS -X POST "${BASE}/api/fuzz/pool/campaigns" \
  -H "Content-Type: application/json" \
  -d "$(python3 - <<PY
import json, os
print(json.dumps({
  "id": "$MAIN_CID",
  "campaign_type": "hunt",
  "title": "hunt bench standard128",
  "status": "running",
  "budget_runs": int(os.environ["POOL_SHARDS"]),
  "budget_seconds": 600,
  "config": {
    "pool_distributed": True,
    "work_kind": "hunt_shard",
    "campaign_type": "hunt",
    "hunt_package": "hunt_standard",
    "upstream_target_id": os.environ["TARGET_ID"],
    "harness_hash": "$HARNESS_HASH",
    "check_semantics": "native_crash",
    "depth_tier": "oss_cve",
    "input_mode": "bytes",
    "iterations_per_shard": int(os.environ["POOL_ITER_PER_SHARD"]),
    "max_input_bytes": 65536,
    "escrow_split": "50_50",
    "bounty_requires_native": True,
    "native_repro_mode": "oss_upstream",
    "hunt_detect_leaks": True,
    "mutator_dict": "7b7d5b5d223a2c6e756c6c7472756566616c73656e756d626572",
  },
}))
PY
)" >/dev/null

WORKERFUZZ_BIN="${WORKERFUZZ_BIN:-$ROOT/bin/workerfuzz-hunt-bench}"
go build -trimpath -o "$WORKERFUZZ_BIN" ./cmd/workerfuzz
export COORD_URL="$BASE"
export COORD_TOKEN="$HACKME_COORDINATOR_WORKER_TOKEN"
WORKERFUZZ_TIMEOUT_MS="${WORKERFUZZ_TIMEOUT_MS:-300000}"
timeout 600s "$WORKERFUZZ_BIN" -coord "$BASE" -token "$HACKME_COORDINATOR_WORKER_TOKEN" \
  -worker hunt-bench-w1 -timeout-ms "$WORKERFUZZ_TIMEOUT_MS" || true

python3 - "$COORD_DB" "$MAIN_CID" "$OUT_JSON" "$POOL_ITER_PER_SHARD" <<'PY'
import json, sqlite3, sys
db, cid, out, pool_iter = sys.argv[1:5]
pool_iter = int(pool_iter)
con = sqlite3.connect(db)
done = con.execute(
    "SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='done' AND result_ok=1",
    (cid,)).fetchone()[0]
cfg_iter = pool_iter
result = {
    "engine": "hunt_pool",
    "campaign_id": cid,
    "shards_done": done,
    "iterations_per_shard": cfg_iter,
    "hunt_package": "hunt_standard",
    "total_shard_execs": done * cfg_iter,
    "target": con.execute(
        "SELECT config_json FROM fuzz_campaigns WHERE id=?", (cid,)).fetchone()[0],
}
import json as j
cfg = j.loads(result.pop("target") or "{}")
result["target"] = cfg.get("upstream_target_id")
with open(out, "w") as f:
    j.dump(result, f, indent=2)
if done < 1:
    raise SystemExit(f"want >=1 shard done got {done}")
print(json.dumps(result))
PY

trap - EXIT
cleanup
