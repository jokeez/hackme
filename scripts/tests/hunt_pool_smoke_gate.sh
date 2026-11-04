#!/usr/bin/env bash
# Gate: Hunt pool shards — worker ASAN smoke (fake-crash rejection: unit TestEvalHuntSubmitRejectsFakeCrash).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

GATE_LOCK="${GATE_LOCK:-/tmp/hackme-hunt-pool-gate.lock}"
exec 9>"$GATE_LOCK"
if ! flock -n 9; then
  echo "[hunt-pool-gate] FAIL: another hunt_pool_smoke_gate is running (lock $GATE_LOCK)" >&2
  exit 1
fi

if ! command -v clang >/dev/null 2>&1; then
  echo "[hunt-pool-gate] SKIP: clang not installed" >&2
  exit 0
fi

echo "[hunt-pool-gate] unit tests (incl. fake-crash replay rejection)"
go test -count=1 ./internal/hunt/... ./internal/poolfuzz/... -run 'Hunt|Replay|Fake'

echo "[hunt-pool-gate] prebuild harness"
go test -count=1 ./internal/hunt -run TestEnsureHarnessBinaryCached -timeout 5m >/dev/null

COORD_DB="${COORD_DB:-$(mktemp "${TMPDIR:-/tmp}/hackme-hunt-pool-gate.XXXXXX.db")}"
FUZZ_DB="${FUZZ_DB:-$(mktemp "${TMPDIR:-/tmp}/hackme-hunt-pool-fuzz.XXXXXX.db")}"
rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true
rm -f "$FUZZ_DB" "${FUZZ_DB}-wal" "${FUZZ_DB}-shm" 2>/dev/null || true
export HACKME_COORDINATOR_DB="$COORD_DB"
export HACKME_COORDINATOR_FUZZ_DB="$FUZZ_DB"
COORD_PORT="${COORD_PORT:-$((18200 + RANDOM % 800))}"
export HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}"
BASE="http://${HACKME_COORDINATOR_ADDR}"
export HACKME_COORDINATOR_ALLOW_INSECURE=1
export HACKME_COORDINATOR_ADMIN_TOKEN=""
export HACKME_COORDINATOR_WORKER_TOKEN="hunt-pool-gate-worker"
export HACKME_POOL_HYBRID_SIGNER_ENABLED=1
export HACKME_POOL_HYBRID_SIGNER_STRICT=1
export HACKME_COORDINATOR_WRITE_TIMEOUT_SEC="${HACKME_COORDINATOR_WRITE_TIMEOUT_SEC:-120}"
export HACKME_POOL_HUNT_REPLAY_MAX_PARALLEL="${HACKME_POOL_HUNT_REPLAY_MAX_PARALLEL:-2}"
export HACKME_POOL_HUNT_REPLAY_ASYNC="${HACKME_POOL_HUNT_REPLAY_ASYNC:-1}"
export HACKME_POOL_HUNT_REPLAY_WORKERS="${HACKME_POOL_HUNT_REPLAY_WORKERS:-2}"

COORD_BIN="${COORD_BIN:-$ROOT/bin/hackme-coordinator-gate}"
echo "[hunt-pool-gate] build coordinator → $COORD_BIN"
go build -trimpath -o "$COORD_BIN" ./cmd/coordinator

echo "[hunt-pool-gate] start coordinator on $HACKME_COORDINATOR_ADDR"
"$COORD_BIN" &
CPID=$!
cleanup_gate() {
  kill "$CPID" 2>/dev/null || true
  wait "$CPID" 2>/dev/null || true
  rm -f "$COORD_DB" "${COORD_DB}-wal" "${COORD_DB}-shm" 2>/dev/null || true
  rm -f "$FUZZ_DB" "${FUZZ_DB}-wal" "${FUZZ_DB}-shm" 2>/dev/null || true
}
trap cleanup_gate EXIT
for _ in $(seq 1 60); do
  if curl -fsS --max-time 2 "${BASE}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.3
done
if ! curl -fsS --max-time 2 "${BASE}/health" >/dev/null 2>&1; then
  echo "[hunt-pool-gate] FAIL: coordinator did not become healthy on $BASE" >&2
  exit 1
fi

TARGET_ID="${HUNT_GATE_TARGET:-jsmn}"
export TARGET_ID
# Coverage leases are ~60–300s; gate uses a short lease so 2 clean shards fit the wall.
GATE_LEASE_SEC="${GATE_LEASE_SEC:-20}"
GATE_MIN_DONE="${GATE_MIN_DONE:-2}"
export GATE_LEASE_SEC GATE_MIN_DONE
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
export HARNESS_HASH

MAIN_CID="hunt-main-$(date +%s)"
export CID="$MAIN_CID"
export BUDGET_RUNS=8

echo "[hunt-pool-gate] register campaign $MAIN_CID"
curl -fsS -X POST "${BASE}/api/fuzz/pool/campaigns" \
  -H "Content-Type: application/json" \
  -d "$(python3 - <<'PY'
import json, os
print(json.dumps({
  "id": os.environ["CID"],
  "campaign_type": "hunt",
  "title": "Hunt pool shard verify",
  "status": "running",
  "budget_runs": int(os.environ["BUDGET_RUNS"]),
  "budget_seconds": 300,
  "config": {
    "pool_distributed": True,
    "work_kind": "hunt_shard",
    "campaign_type": "hunt",
    "hunt_package": "hunt_lite",
    "hunt_engine": "libfuzzer",
    "engine_lease_sec": int(os.environ.get("GATE_LEASE_SEC", "20")),
    "upstream_target_id": os.environ["TARGET_ID"],
    "harness_hash": os.environ["HARNESS_HASH"],
    "check_semantics": "native_crash",
    "depth_tier": "oss_cve",
    "input_mode": "bytes",
    "iterations_per_shard": 2,
    "max_input_bytes": 256,
    "escrow_split": "50_50",
    "bounty_requires_native": True,
    "native_repro_mode": "oss_upstream",
  },
}))
PY
)" >/dev/null

HARNESS_BIN="$ROOT/.cache/hunt-harness/${HARNESS_HASH}.bin"
if [[ ! -f "$HARNESS_BIN" ]]; then
  echo "[hunt-pool-gate] FAIL: harness missing at $HARNESS_BIN (prebuild should have created it)" >&2
  exit 1
fi
echo "[hunt-pool-gate] publish harness $HARNESS_HASH ($(stat -c%s "$HARNESS_BIN") bytes)"
B64_FILE="$(mktemp)"
JSON_FILE="$(mktemp)"
base64 -w0 "$HARNESS_BIN" >"$B64_FILE"
jq -nc --arg h "$HARNESS_HASH" --rawfile b "$B64_FILE" --arg s "gate:${TARGET_ID}" \
  '{harness_hash:$h, source_rel:$s, binary_b64:$b}' >"$JSON_FILE"
curl -fsS -X POST "${BASE}/api/fuzz/pool/hunt/harness" \
  -H "Content-Type: application/json" \
  -d @"$JSON_FILE" | jq -e '.ok == true' >/dev/null
rm -f "$B64_FILE" "$JSON_FILE"

WORKERFUZZ_BIN="${WORKERFUZZ_BIN:-$ROOT/bin/workerfuzz-hunt-gate}"
echo "[hunt-pool-gate] build workerfuzz → $WORKERFUZZ_BIN"
go build -trimpath -o "$WORKERFUZZ_BIN" ./cmd/workerfuzz
export COORD_URL="$BASE"
export COORD_TOKEN="$HACKME_COORDINATOR_WORKER_TOKEN"
export WORKERFUZZ_TIMEOUT_MS="${WORKERFUZZ_TIMEOUT_MS:-120000}"
# Nested userns (Cursor/CI agents) often deny bwrap uid_map while leaving bwrap on PATH.
# Fleet/Vast miners use real userns — keep STRICT=1 there. Desk gate: soft-relax so shards exercise Dig/Hunt paths.
if [[ -z "${HACKME_HUNT_ISOLATE_STRICT:-}" ]]; then
  if command -v bwrap >/dev/null 2>&1 && bwrap --ro-bind / / --dev /dev --proc /proc --unshare-pid --unshare-net --die-with-parent -- "$(command -v true 2>/dev/null || echo /bin/true)" >/dev/null 2>&1; then
    export HACKME_HUNT_ISOLATE_STRICT=1
    export HACKME_HUNT_ISOLATE_REQUIRE=1
    echo "[hunt-pool-gate] bwrap probe OK — STRICT isolate ON"
  else
    export HACKME_HUNT_ISOLATE_STRICT=0
    export HACKME_HUNT_ISOLATE_REQUIRE=0
    echo "[hunt-pool-gate] WARN: bwrap probe failed/missing — STRICT=0 for desk gate only (Vast must re-run with working userns)"
  fi
fi
# workerfuzz signs claims (hybrid signer): give the gate miner an ephemeral identity.
if [[ -z "${HACKME_MINER_ED25519_SEED_HEX:-}" && -z "${HACKME_MINER_SEED_FILE:-}" ]]; then
  HACKME_MINER_ED25519_SEED_HEX="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
  export HACKME_MINER_ED25519_SEED_HEX
fi
echo "[hunt-pool-gate] run workerfuzz (timeout_ms=$WORKERFUZZ_TIMEOUT_MS lease=${GATE_LEASE_SEC}s min_done=${GATE_MIN_DONE} isolate_strict=${HACKME_HUNT_ISOLATE_STRICT})"
# Wall ≥ 2 coverage leases + drain slack (was fixed 120s wanting 4×60s → false FAIL).
GATE_WALL_SEC="${GATE_WALL_SEC:-$((GATE_LEASE_SEC * GATE_MIN_DONE + 45))}"
set +e
WORKER_ID=hunt-w1 timeout "$GATE_WALL_SEC"s "$WORKERFUZZ_BIN" -coord "$BASE" -token "$HACKME_COORDINATOR_WORKER_TOKEN" -worker hunt-w1 -timeout-ms "$WORKERFUZZ_TIMEOUT_MS"
WF_EC=$?
set -e
# timeout exits 124; worker may exit 0 after wall — both OK if shards complete below
if [[ "$WF_EC" -ne 0 && "$WF_EC" -ne 124 ]]; then
  echo "[hunt-pool-gate] WARN: workerfuzz exit=$WF_EC (continuing to shard check)" >&2
fi

echo "[hunt-pool-gate] wait for async replay drain"
for _ in $(seq 1 120); do
  PENDING="$(python3 - "$FUZZ_DB" <<'PY'
import sqlite3, sys
db = sys.argv[1]
con = sqlite3.connect(db)
n = con.execute("SELECT COUNT(*) FROM fuzz_work_items WHERE status='replay_pending'").fetchone()[0]
q = con.execute("SELECT COUNT(*) FROM fuzz_hunt_replay_queue WHERE status IN ('pending','processing')").fetchone()[0]
print(n + q)
PY
)"
  if [[ "${PENDING:-1}" == "0" ]]; then
    break
  fi
  sleep 0.5
done

DONE="$(python3 - "$FUZZ_DB" "$MAIN_CID" "$GATE_MIN_DONE" <<'PY'
import sqlite3, sys
db, cid, want = sys.argv[1], sys.argv[2], int(sys.argv[3])
con = sqlite3.connect(db)
n = con.execute("SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='done' AND result_ok=1", (cid,)).fetchone()[0]
print(n)
if n < want:
    raise SystemExit(f"want >={want} clean shards got {n} (coverage lease gate — not legacy 4×byte)")
PY
)"
echo "[hunt-pool-gate] clean shards done=$DONE (min=${GATE_MIN_DONE})"

echo "[hunt-pool-gate] PASS"
trap - EXIT
cleanup_gate
