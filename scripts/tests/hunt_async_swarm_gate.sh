#!/usr/bin/env bash
# Hunt async replay swarm calibration — parallel workerfuzz + queue depth / drain metrics.
#
# Usage:
#   bash scripts/tests/hunt_async_swarm_gate.sh
# Env:
#   SWARM_WORKERS=12          — parallel miner workers (default 12)
#   SHARDS_PER_WORKER=6       — target shards each (budget = workers * this)
#   DEMO_SEC=180              — wall clock per run
#   REPLAY_WORKERS=4          — HACKME_POOL_HUNT_REPLAY_WORKERS
#   REPLAY_MAX_PARALLEL=3     — HACKME_POOL_HUNT_REPLAY_MAX_PARALLEL
#   ITER_PER_SHARD=2          — fast shard (smoke-like)
#   SWEEP_REPLAY_WORKERS=     — comma list e.g. "2,4,6,8" to sweep (overrides single run)
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

GATE_LOCK="${GATE_LOCK:-/tmp/hackme-hunt-async-swarm.lock}"
exec 9>"$GATE_LOCK"
if ! flock -n 9; then
  echo "[hunt-async-swarm] FAIL: another run holds $GATE_LOCK" >&2
  exit 1
fi

if ! command -v clang >/dev/null 2>&1; then
  echo "[hunt-async-swarm] SKIP: clang not installed" >&2
  exit 0
fi

SWARM_WORKERS="${SWARM_WORKERS:-12}"
SHARDS_PER_WORKER="${SHARDS_PER_WORKER:-6}"
DEMO_SEC="${DEMO_SEC:-180}"
REPLAY_WORKERS="${REPLAY_WORKERS:-4}"
REPLAY_MAX_PARALLEL="${REPLAY_MAX_PARALLEL:-3}"
ITER_PER_SHARD="${ITER_PER_SHARD:-2}"
TARGET_ID="${HUNT_GATE_TARGET:-jsmn}"
export TARGET_ID
export BUDGET_RUNS=$((SWARM_WORKERS * SHARDS_PER_WORKER))
export ITER_PER_SHARD
TS="$(date -u +%Y%m%dT%H%M%SZ)"
REPORT_DIR="${REPORT_DIR:-$ROOT/reports/hunt-async-swarm/$TS}"
mkdir -p "$REPORT_DIR"

COORD_BIN="${COORD_BIN:-$ROOT/bin/hackme-coordinator-swarm}"
WORKERFUZZ_BIN="${WORKERFUZZ_BIN:-$ROOT/bin/workerfuzz-hunt-swarm}"
echo "[hunt-async-swarm] build binaries"
go build -trimpath -o "$COORD_BIN" ./cmd/coordinator
go build -trimpath -o "$WORKERFUZZ_BIN" ./cmd/workerfuzz

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

run_profile() {
  local rw="$1"
  local rmp="$2"
  local tag="rw${rw}_mp${rmp}_w${SWARM_WORKERS}"
  local out="$REPORT_DIR/${tag}"
  mkdir -p "$out"

  local coord_db fuzz_db
  coord_db="$(mktemp "${TMPDIR:-/tmp}/hackme-swarm-coord.XXXXXX.db")"
  fuzz_db="$(mktemp "${TMPDIR:-/tmp}/hackme-swarm-fuzz.XXXXXX.db")"
  rm -f "$coord_db" "${coord_db}-wal" "${coord_db}-shm" "$fuzz_db" "${fuzz_db}-wal" "${fuzz_db}-shm" 2>/dev/null || true

  local port base
  port=$((20000 + RANDOM % 4000))
  base="http://127.0.0.1:${port}"

  export HACKME_COORDINATOR_DB="$coord_db"
  export HACKME_COORDINATOR_FUZZ_DB="$fuzz_db"
  export HACKME_COORDINATOR_ADDR="127.0.0.1:${port}"
  export HACKME_COORDINATOR_ALLOW_INSECURE=1
  export HACKME_COORDINATOR_ADMIN_TOKEN=""
  export HACKME_COORDINATOR_WORKER_TOKEN="hunt-swarm-worker"
  export HACKME_POOL_HYBRID_SIGNER_ENABLED=1
  export HACKME_POOL_HYBRID_SIGNER_STRICT=1
  export HACKME_COORDINATOR_WRITE_TIMEOUT_SEC=120
  export HACKME_POOL_HUNT_REPLAY_ASYNC=1
  export HACKME_POOL_HUNT_REPLAY_WORKERS="$rw"
  export HACKME_POOL_HUNT_REPLAY_MAX_PARALLEL="$rmp"
  export HACKME_WORKER_SKIP_INSTANCE_LOCK=1
  export WORKERFUZZ_TIMEOUT_MS="${WORKERFUZZ_TIMEOUT_MS:-120000}"

  echo "[hunt-async-swarm] profile $tag replay_workers=$rw max_parallel=$rmp miners=$SWARM_WORKERS budget=$BUDGET_RUNS"
  "$COORD_BIN" >"$out/coordinator.log" 2>&1 &
  local cpid=$!
  cleanup_profile() {
    kill "$cpid" 2>/dev/null || true
    wait "$cpid" 2>/dev/null || true
    rm -f "$coord_db" "${coord_db}-wal" "${coord_db}-shm" "$fuzz_db" "${fuzz_db}-wal" "${fuzz_db}-shm" 2>/dev/null || true
  }
  trap cleanup_profile RETURN

  for _ in $(seq 1 60); do
    curl -fsS --max-time 2 "${base}/health" >/dev/null 2>&1 && break
    sleep 0.3
  done
  if ! curl -fsS --max-time 2 "${base}/health" >/dev/null 2>&1; then
    echo "[hunt-async-swarm] FAIL: coordinator unhealthy" >&2
    return 1
  fi

  local cid="hunt-swarm-$(date +%s)"
  curl -fsS -X POST "${base}/api/fuzz/pool/campaigns" \
    -H "Content-Type: application/json" \
    -d "$(python3 - <<PY
import json, os
print(json.dumps({
  "id": "$cid",
  "campaign_type": "hunt",
  "title": "hunt async swarm $tag",
  "status": "running",
  "budget_runs": int(os.environ["BUDGET_RUNS"]),
  "budget_seconds": 900,
  "config": {
    "pool_distributed": True,
    "work_kind": "hunt_shard",
    "campaign_type": "hunt",
    "upstream_target_id": os.environ["TARGET_ID"],
    "harness_hash": "$HARNESS_HASH",
    "check_semantics": "native_crash",
    "depth_tier": "oss_cve",
    "input_mode": "bytes",
    "iterations_per_shard": int(os.environ["ITER_PER_SHARD"]),
    "max_input_bytes": 256,
    "escrow_split": "50_50",
    "bounty_requires_native": True,
    "native_repro_mode": "oss_upstream",
  },
}))
PY
)" >/dev/null

  local pids=()
  local w
  for w in $(seq 1 "$SWARM_WORKERS"); do
    local wid="hunt-swarm-$(printf '%03d' "$w")"
    timeout "$DEMO_SEC"s "$WORKERFUZZ_BIN" \
      -coord "$base" -token "$HACKME_COORDINATOR_WORKER_TOKEN" \
      -worker "$wid" -timeout-ms "$WORKERFUZZ_TIMEOUT_MS" \
      >"$out/${wid}.log" 2>&1 &
    pids+=($!)
  done

  local samples="$out/samples.tsv"
  echo -e "ts\tqueue_pending\tqueue_processing\twork_replay_pending\tdone_shards" >"$samples"
  local start_ts end_ts
  start_ts=$(date +%s)

  (
    while kill -0 "$cpid" 2>/dev/null; do
      python3 - "$fuzz_db" "$cid" >>"$samples" 2>/dev/null <<'PY' || true
import sqlite3, sys, time
db, cid = sys.argv[1], sys.argv[2]
con = sqlite3.connect(db)
qp = con.execute("SELECT COUNT(*) FROM fuzz_hunt_replay_queue WHERE status='pending'").fetchone()[0]
qpr = con.execute("SELECT COUNT(*) FROM fuzz_hunt_replay_queue WHERE status='processing'").fetchone()[0]
wrp = con.execute("SELECT COUNT(*) FROM fuzz_work_items WHERE status='replay_pending'").fetchone()[0]
done = con.execute("SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='done'", (cid,)).fetchone()[0]
print(f"{int(time.time())}\t{qp}\t{qpr}\t{wrp}\t{done}")
PY
      sleep 1
    done
  ) &
  local mon_pid=$!

  local wp
  for wp in "${pids[@]}"; do
    wait "$wp" 2>/dev/null || true
  done
  end_ts=$(date +%s)
  kill "$mon_pid" 2>/dev/null || true
  wait "$mon_pid" 2>/dev/null || true

  echo "[hunt-async-swarm] miners finished; draining replay queue"
  local drain_start drain_end
  drain_start=$(date +%s)
  for _ in $(seq 1 300); do
    local pending
    pending="$(python3 - "$fuzz_db" <<'PY'
import sqlite3, sys
con = sqlite3.connect(sys.argv[1])
n = con.execute("SELECT COUNT(*) FROM fuzz_work_items WHERE status='replay_pending'").fetchone()[0]
q = con.execute("SELECT COUNT(*) FROM fuzz_hunt_replay_queue WHERE status IN ('pending','processing')").fetchone()[0]
print(n + q)
PY
)"
    if [[ "${pending:-1}" == "0" ]]; then
      break
    fi
    sleep 0.5
  done
  drain_end=$(date +%s)

  curl -fsS "${base}/api/fuzz/pool/stats" >"$out/pool_stats.json" 2>/dev/null || echo '{}' >"$out/pool_stats.json"

  python3 - "$fuzz_db" "$cid" "$out/metrics.json" "$tag" "$rw" "$rmp" "$SWARM_WORKERS" "$BUDGET_RUNS" \
    "$start_ts" "$end_ts" "$drain_start" "$drain_end" "$samples" <<'PY'
import json, sqlite3, sys

db, cid, out_path = sys.argv[1], sys.argv[2], sys.argv[3]
tag, rw, rmp, swarm_w, budget = sys.argv[4], sys.argv[5], sys.argv[6], sys.argv[7], sys.argv[8]
start_ts, end_ts, drain_start, drain_end = map(int, sys.argv[9:13])
samples_path = sys.argv[13]

con = sqlite3.connect(db)
done_ok = con.execute(
    "SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='done' AND result_ok=1", (cid,)
).fetchone()[0]
done_all = con.execute(
    "SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='done'", (cid,)
).fetchone()[0]
failed_q = con.execute(
    "SELECT COUNT(*) FROM fuzz_hunt_replay_queue WHERE campaign_id=? AND status='failed'", (cid,)
).fetchone()[0]
stuck_replay = con.execute(
    "SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='replay_pending'", (cid,)
).fetchone()[0]

max_qp = max_qpr = max_wrp = 0
try:
    with open(samples_path) as f:
        next(f, None)
        for line in f:
            parts = line.strip().split("\t")
            if len(parts) < 5:
                continue
            max_qp = max(max_qp, int(parts[1]))
            max_qpr = max(max_qpr, int(parts[2]))
            max_wrp = max(max_wrp, int(parts[3]))
except FileNotFoundError:
    pass

miner_sec = max(1, end_ts - start_ts)
drain_sec = max(0, drain_end - drain_start)
metrics = {
    "tag": tag,
    "replay_workers": int(rw),
    "replay_max_parallel": int(rmp),
    "swarm_workers": int(swarm_w),
    "budget_runs": int(budget),
    "shards_done_ok": done_ok,
    "shards_done_all": done_all,
    "replay_failed": failed_q,
    "stuck_replay_pending": stuck_replay,
    "max_queue_pending": max_qp,
    "max_queue_processing": max_qpr,
    "max_work_replay_pending": max_wrp,
    "miner_wall_sec": miner_sec,
    "drain_sec": drain_sec,
    "shards_per_sec": round(done_ok / miner_sec, 3),
    "pass": stuck_replay == 0 and failed_q == 0 and done_ok >= int(budget) * 0.5,
}
with open(out_path, "w") as f:
    json.dump(metrics, f, indent=2)
print(json.dumps(metrics))
if not metrics["pass"]:
    raise SystemExit(1)
PY

  trap - RETURN
  cleanup_profile
}

echo "[hunt-async-swarm] unit smoke"
go test -count=1 ./internal/poolfuzz/... -run 'HuntReplayAsync' -timeout 3m >/dev/null

if [[ -n "${SWEEP_REPLAY_WORKERS:-}" ]]; then
  IFS=',' read -r -a sweep <<<"$SWEEP_REPLAY_WORKERS"
  for rw in "${sweep[@]}"; do
    run_profile "$rw" "$REPLAY_MAX_PARALLEL"
  done
else
  run_profile "$REPLAY_WORKERS" "$REPLAY_MAX_PARALLEL"
fi

python3 - "$REPORT_DIR" <<'PY'
import json, pathlib, sys

root = pathlib.Path(sys.argv[1])
rows = []
for p in sorted(root.glob("rw*/metrics.json")):
    rows.append(json.loads(p.read_text()))
summary = {
    "profiles": rows,
    "recommended": None,
}
ok = [r for r in rows if r.get("pass")]
if ok:
    # Prefer lowest replay_workers that keeps max_queue_pending <= swarm_workers/2 and drain < 60s
    ok.sort(key=lambda r: (r["max_queue_pending"], r["drain_sec"], r["replay_workers"]))
    best = ok[0]
    summary["recommended"] = {
        "HACKME_POOL_HUNT_REPLAY_WORKERS": best["replay_workers"],
        "HACKME_POOL_HUNT_REPLAY_MAX_PARALLEL": best["replay_max_parallel"],
        "reason": f"max_queue={best['max_queue_pending']} drain={best['drain_sec']}s shards_ok={best['shards_done_ok']}",
    }
(root / "SUMMARY.json").write_text(json.dumps(summary, indent=2))
print(json.dumps(summary, indent=2))
if not ok:
    raise SystemExit("no passing profile")
PY

echo "[hunt-async-swarm] PASS report=$REPORT_DIR"
