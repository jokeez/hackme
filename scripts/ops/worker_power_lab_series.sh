#!/usr/bin/env bash
set -euo pipefail

# worker_power_lab_series.sh
# Runs several worker_power_lab scenarios and builds one comparison report.
#
# Usage:
#   ADMIN_TOKEN=... COORD=http://127.0.0.1:18081 BASE=http://127.0.0.1:18080 \
#   DURATION_SEC=90 bash scripts/ops/worker_power_lab_series.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[worker-power-series] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd jq
require_cmd python3

ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
BASE="${BASE:-http://127.0.0.1:18080}"
COORD="${COORD:-http://127.0.0.1:18081}"
DURATION_SEC="${DURATION_SEC:-90}"
POLL_SEC="${POLL_SEC:-15}"
SERIES_ID="${SERIES_ID:-worker_power_series_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests/$SERIES_ID}"
mkdir -p "$OUT_DIR"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[worker-power-series] ADMIN_TOKEN is required" >&2
  exit 2
fi

echo "[worker-power-series] SERIES_ID=${SERIES_ID}"
echo "[worker-power-series] BASE=${BASE} COORD=${COORD} DURATION_SEC=${DURATION_SEC}"

declare -a CASE_IDS=(
  "balanced"
  "cpu_heavy"
  "gpu_heavy"
)
declare -A CASE_SPEC
CASE_SPEC["balanced"]="cpu-low:262144:cpu,cpu-mid:1048576:cpu,cpu-high:2097152:cpu,gpu-main:4194304:gpu"
CASE_SPEC["cpu_heavy"]="cpu-low:1048576:cpu,cpu-mid:2097152:cpu,cpu-high:4194304:cpu,gpu-main:2097152:gpu"
CASE_SPEC["gpu_heavy"]="cpu-low:262144:cpu,cpu-mid:524288:cpu,cpu-high:1048576:cpu,gpu-main:8388608:gpu"

results_jsonl="$OUT_DIR/results.jsonl"
: >"$results_jsonl"

for case_id in "${CASE_IDS[@]}"; do
  spec="${CASE_SPEC[$case_id]}"
  case_out="$OUT_DIR/$case_id"
  mkdir -p "$case_out"
  echo "[worker-power-series] case=${case_id} spec=${spec}"
  if env \
    ADMIN_TOKEN="$ADMIN_TOKEN" \
    BASE="$BASE" \
    COORD="$COORD" \
    SKIP_BASE_STATUS_CHECK=1 \
    DURATION_SEC="$DURATION_SEC" \
    POLL_SEC="$POLL_SEC" \
    WORKERS_SPEC="$spec" \
    OUT_DIR="$case_out" \
    LAB_ID="${SERIES_ID}_${case_id}" \
    bash scripts/ops/worker_power_lab.sh >"$case_out/run.log" 2>&1; then
    verdict="pass"
  else
    verdict="fail"
  fi
  jq -nc \
    --arg case_id "$case_id" \
    --arg spec "$spec" \
    --arg verdict "$verdict" \
    --arg summary "$case_out/summary.json" \
    --arg log "$case_out/run.log" \
    '{case_id:$case_id,spec:$spec,verdict:$verdict,summary:$summary,log:$log}' >>"$results_jsonl"
done

python3 - "$OUT_DIR" <<'PY'
import json
import pathlib
import sys

out = pathlib.Path(sys.argv[1])
rows = []
for line in (out / "results.jsonl").read_text(encoding="utf-8").splitlines():
    if not line.strip():
        continue
    item = json.loads(line)
    sfile = pathlib.Path(item["summary"])
    if item["verdict"] != "pass" or not sfile.exists():
        rows.append({
            "case_id": item["case_id"],
            "verdict": item["verdict"],
            "error": "case failed or summary missing",
        })
        continue
    summary = json.loads(sfile.read_text(encoding="utf-8"))
    workers = summary.get("workers") or []
    gpu_share = 0.0
    cpu_share = 0.0
    for w in workers:
        wid = str(w.get("worker_id", ""))
        share = float(w.get("attempts_share_pct") or 0.0)
        if "gpu" in wid:
            gpu_share += share
        else:
            cpu_share += share
    rows.append({
        "case_id": item["case_id"],
        "verdict": item["verdict"],
        "total_accepted_attempts_delta": int(summary.get("total_accepted_attempts_delta") or 0),
        "total_payout_hmc_delta": float(summary.get("total_payout_hmc_delta") or 0.0),
        "gpu_attempts_share_pct": round(gpu_share, 4),
        "cpu_attempts_share_pct": round(cpu_share, 4),
        "workers": workers,
    })

pass_count = sum(1 for r in rows if r.get("verdict") == "pass")
overall = {
    "gate": "worker_power_lab_series_v1",
    "pass": pass_count == len(rows),
    "cases_total": len(rows),
    "cases_pass": pass_count,
    "cases": rows,
}
(out / "comparison_summary.json").write_text(json.dumps(overall, indent=2), encoding="utf-8")
print(json.dumps(overall, indent=2))
PY

echo "[worker-power-series] summary: $OUT_DIR/comparison_summary.json"
