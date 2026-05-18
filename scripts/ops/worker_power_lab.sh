#!/usr/bin/env bash
set -euo pipefail

# worker_power_lab.sh
# Multi-worker synthetic power lab for realistic coordinator behavior:
# - spawns several workerpoh processes with different "power" profiles
# - supports CPU-only workers and (optional) one GPU worker
# - measures distribution of accepted attempts and payout deltas
#
# Usage example:
#   ADMIN_TOKEN=... BASE=http://127.0.0.1:18080 COORD=http://127.0.0.1:18081 \
#   DURATION_SEC=240 bash scripts/ops/worker_power_lab.sh
#
# Profiles format (WORKERS_SPEC):
#   worker_id:batch:mode
# where mode is cpu|gpu
# Example:
#   WORKERS_SPEC="cpu-low:262144:cpu,cpu-mid:1048576:cpu,gpu-main:4194304:gpu"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[worker-power-lab] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd curl
require_cmd jq
require_cmd python3
require_cmd go

BASE="${BASE:-http://127.0.0.1:18080}"
COORD="${COORD:-http://127.0.0.1:18081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
SKIP_BASE_STATUS_CHECK="${SKIP_BASE_STATUS_CHECK:-0}"
DURATION_SEC="${DURATION_SEC:-180}"
POLL_SEC="${POLL_SEC:-15}"
GPU_CHUNK="${GPU_CHUNK:-4194304}"
SEARCH_TIMEOUT_MS="${SEARCH_TIMEOUT_MS:-2500}"
WORKERS_SPEC="${WORKERS_SPEC:-cpu-low:262144:cpu,cpu-mid:1048576:cpu,cpu-high:2097152:cpu,gpu-main:4194304:gpu}"
LAB_ID="${LAB_ID:-worker_power_lab_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests/$LAB_ID}"
mkdir -p "$OUT_DIR"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[worker-power-lab] ADMIN_TOKEN is required" >&2
  exit 2
fi

if ! [[ "$DURATION_SEC" =~ ^[0-9]+$ ]] || (( DURATION_SEC < 30 )); then
  echo "[worker-power-lab] DURATION_SEC must be integer >= 30" >&2
  exit 2
fi
if ! [[ "$POLL_SEC" =~ ^[0-9]+$ ]] || (( POLL_SEC < 2 )); then
  echo "[worker-power-lab] POLL_SEC must be integer >= 2" >&2
  exit 2
fi

echo "[worker-power-lab] LAB_ID=${LAB_ID}"
echo "[worker-power-lab] BASE=${BASE} COORD=${COORD} DURATION_SEC=${DURATION_SEC}"
echo "[worker-power-lab] WORKERS_SPEC=${WORKERS_SPEC}"

curl -fsS --max-time 10 "${COORD}/api/work/stats" >/dev/null
if [[ "${SKIP_BASE_STATUS_CHECK}" != "1" ]]; then
  curl -fsS --max-time 10 "${BASE}/api/status" >/dev/null
fi

WORKER_BIN="$ROOT_DIR/bin/workerpoh-opencl"
if [[ ! -x "$WORKER_BIN" ]]; then
  echo "[worker-power-lab] building worker binary: $WORKER_BIN"
  go build -tags opencl -o "$WORKER_BIN" ./cmd/workerpoh
fi

declare -a PIDS=()
declare -a WORKER_IDS=()
declare -a WORKER_BATCH=()
declare -a WORKER_MODE=()
declare -a WORKER_SEEDS=()

cleanup() {
  for pid in "${PIDS[@]:-}"; do
    if [[ -n "${pid:-}" ]] && kill -0 "$pid" >/dev/null 2>&1; then
      kill "$pid" >/dev/null 2>&1 || true
    fi
  done
}
trap cleanup EXIT INT TERM

echo "[worker-power-lab] snapshot: before"
curl -fsS --max-time 15 "${COORD}/api/work/stats?details=1" >"$OUT_DIR/before_work_stats.json"
curl -fsS --max-time 15 "${BASE}/api/worker/settlement" >"$OUT_DIR/before_settlement.json" || true

parse_workers() {
  local spec="$1"
  IFS=',' read -r -a arr <<<"$spec"
  if (( ${#arr[@]} == 0 )); then
    echo "[worker-power-lab] empty WORKERS_SPEC" >&2
    exit 2
  fi
  for item in "${arr[@]}"; do
    item="$(echo "$item" | xargs)"
    [[ -z "$item" ]] && continue
    IFS=':' read -r wid batch mode <<<"$item"
    wid="$(echo "${wid:-}" | xargs)"
    batch="$(echo "${batch:-}" | xargs)"
    mode="$(echo "${mode:-cpu}" | xargs | tr '[:upper:]' '[:lower:]')"
    if [[ -z "$wid" || -z "$batch" ]]; then
      echo "[worker-power-lab] invalid WORKERS_SPEC entry: $item" >&2
      exit 2
    fi
    if ! [[ "$batch" =~ ^[0-9]+$ ]] || (( batch < 1024 )); then
      echo "[worker-power-lab] invalid batch for ${wid}: ${batch}" >&2
      exit 2
    fi
    if [[ "$mode" != "cpu" && "$mode" != "gpu" ]]; then
      echo "[worker-power-lab] invalid mode for ${wid}: ${mode} (use cpu|gpu)" >&2
      exit 2
    fi
    WORKER_IDS+=("$wid")
    WORKER_BATCH+=("$batch")
    WORKER_MODE+=("$mode")
  done
}

parse_workers "$WORKERS_SPEC"

for i in "${!WORKER_IDS[@]}"; do
  wid="${WORKER_IDS[$i]}"
  batch="${WORKER_BATCH[$i]}"
  mode="${WORKER_MODE[$i]}"
  seed_json="$(go run ./cmd/minersign -gen-seed)"
  seed="$(printf '%s' "$seed_json" | jq -r '.HACKME_MINER_ED25519_SEED_HEX // ""')"
  if [[ -z "$seed" ]]; then
    echo "[worker-power-lab] failed to generate seed for ${wid}" >&2
    exit 2
  fi
  WORKER_SEEDS+=("$seed")
  log_file="$OUT_DIR/${wid}.log"
  nonce_file="$OUT_DIR/${wid}.nonce.seq"
  gpu_flags=()
  if [[ "$mode" == "cpu" ]]; then
    gpu_flags+=("-gpu-disable")
  else
    gpu_flags+=("-gpu-backend" "opencl")
  fi
  echo "[worker-power-lab] start worker=${wid} mode=${mode} batch=${batch}"
  HACKME_MINER_ED25519_SEED_HEX="$seed" \
    HACKME_MINER_NONCE_FILE="$nonce_file" \
    "$WORKER_BIN" \
    -coord "$COORD" \
    -token "$ADMIN_TOKEN" \
    -worker "$wid" \
    -batch "$batch" \
    -gpu-chunk "$GPU_CHUNK" \
    -search-timeout-ms "$SEARCH_TIMEOUT_MS" \
    "${gpu_flags[@]}" \
    >"$log_file" 2>&1 &
  PIDS+=("$!")
done

echo "[worker-power-lab] all workers started: count=${#PIDS[@]}"

elapsed=0
while (( elapsed < DURATION_SEC )); do
  sleep "$POLL_SEC"
  elapsed=$((elapsed + POLL_SEC))
  if ! curl -fsS --max-time 10 "${COORD}/api/work/stats?details=1" >"$OUT_DIR/live_work_stats_${elapsed}s.json"; then
    echo "[worker-power-lab] warn: failed live stats poll at t=${elapsed}s"
  fi
  # quick death check
  for i in "${!PIDS[@]}"; do
    pid="${PIDS[$i]}"
    wid="${WORKER_IDS[$i]}"
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      echo "[worker-power-lab] warn: worker ${wid} exited early (pid=${pid})"
    fi
  done
  echo "[worker-power-lab] t=${elapsed}s/${DURATION_SEC}s"
done

echo "[worker-power-lab] snapshot: after"
curl -fsS --max-time 15 "${COORD}/api/work/stats?details=1" >"$OUT_DIR/after_work_stats.json"
curl -fsS --max-time 15 "${BASE}/api/worker/settlement" >"$OUT_DIR/after_settlement.json" || true

# stop workers after final snapshot
cleanup
PIDS=()

python3 - "$OUT_DIR" <<'PY'
import json
import pathlib
import sys

out_dir = pathlib.Path(sys.argv[1])
before = json.loads((out_dir / "before_work_stats.json").read_text(encoding="utf-8"))
after = json.loads((out_dir / "after_work_stats.json").read_text(encoding="utf-8"))

def workers_map(doc):
    w = doc.get("workers")
    if isinstance(w, dict):
        return w
    return {}

bw = workers_map(before)
aw = workers_map(after)
all_ids = sorted(set(bw.keys()) | set(aw.keys()))
rows = []
total_attempts_delta = 0
total_payout_delta = 0.0

for wid in all_ids:
    b = bw.get(wid, {}) or {}
    a = aw.get(wid, {}) or {}
    b_attempts = int(b.get("accepted_attempts") or 0)
    a_attempts = int(a.get("accepted_attempts") or 0)
    b_payout = float(b.get("payout_hmc") or 0.0)
    a_payout = float(a.get("payout_hmc") or 0.0)
    d_attempts = a_attempts - b_attempts
    d_payout = a_payout - b_payout
    total_attempts_delta += max(d_attempts, 0)
    total_payout_delta += max(d_payout, 0.0)
    rows.append({
        "worker_id": wid,
        "accepted_attempts_before": b_attempts,
        "accepted_attempts_after": a_attempts,
        "accepted_attempts_delta": d_attempts,
        "payout_hmc_before": b_payout,
        "payout_hmc_after": a_payout,
        "payout_hmc_delta": d_payout,
        "last_hashrate_gh_s_after": float(a.get("hashrate_gh_s") or 0.0),
    })

for r in rows:
    if total_attempts_delta > 0:
        r["attempts_share_pct"] = round((max(r["accepted_attempts_delta"], 0) / total_attempts_delta) * 100.0, 4)
    else:
        r["attempts_share_pct"] = 0.0
    if total_payout_delta > 0:
        r["payout_share_pct"] = round((max(r["payout_hmc_delta"], 0.0) / total_payout_delta) * 100.0, 4)
    else:
        r["payout_share_pct"] = 0.0

summary = {
    "gate": "worker_power_lab_v1",
    "pass": True,
    "workers_count": len(rows),
    "total_accepted_attempts_delta": total_attempts_delta,
    "total_payout_hmc_delta": round(total_payout_delta, 12),
    "workers": sorted(rows, key=lambda x: x["accepted_attempts_delta"], reverse=True),
}

(out_dir / "summary.json").write_text(json.dumps(summary, indent=2), encoding="utf-8")
print(json.dumps(summary, indent=2))
PY

echo "[worker-power-lab] summary: $OUT_DIR/summary.json"
