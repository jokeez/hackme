#!/usr/bin/env bash
set -euo pipefail

# top_pool_readiness_gate.sh
# KPI gate for "can we call this a top pool?"
#
# Usage:
#   PROFILE=canary BASE=http://127.0.0.1:18080 COORD=http://127.0.0.1:18081 \
#   bash scripts/ops/top_pool_readiness_gate.sh
#
# Profiles:
# - canary: minimal operational confidence
# - top: target thresholds for top-pool claim
#
# Optional:
#   SETTLEMENT_RELAXED=1 — if GET $BASE/api/worker/settlement is missing (403 behind nginx) or
#   non-JSON, waive settlement-related checks (deploy scripts/ops/nginx/hackme-site-domain.tls.conf
#   with worker/settlement exposed instead).

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[top-pool-gate] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq
require_cmd python3

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

PROFILE="${PROFILE:-canary}"
BASE="${BASE:-http://127.0.0.1:18080}"
COORD="${COORD:-http://127.0.0.1:18081}"

RUN_ID="${RUN_ID:-top_pool_gate_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/gates/$RUN_ID}"
mkdir -p "$OUT_DIR"
SUMMARY="$OUT_DIR/summary.json"

# Profile defaults (can be overridden by explicit env vars).
if [[ "$PROFILE" == "top" ]]; then
  default_min_workers=10
  default_min_blocks_1h=100
  default_max_obs_block_sec=35
  default_max_unpaid_hmc=0.5
  default_max_sweep_eta_sec=90000
  default_min_payout_hmc=0.01
else
  default_min_workers=2
  default_min_blocks_1h=60
  default_max_obs_block_sec=45
  default_max_unpaid_hmc=1.0
  default_max_sweep_eta_sec=93600
  default_min_payout_hmc=0.0001
fi

MIN_WORKERS="${MIN_WORKERS:-$default_min_workers}"
MIN_BLOCKS_1H="${MIN_BLOCKS_1H:-$default_min_blocks_1h}"
MAX_OBS_BLOCK_SEC="${MAX_OBS_BLOCK_SEC:-$default_max_obs_block_sec}"
MAX_UNPAID_HMC="${MAX_UNPAID_HMC:-$default_max_unpaid_hmc}"
MAX_SWEEP_ETA_SEC="${MAX_SWEEP_ETA_SEC:-$default_max_sweep_eta_sec}"
MIN_PAYOUT_HMC="${MIN_PAYOUT_HMC:-$default_min_payout_hmc}"
SETTLEMENT_RELAXED="${SETTLEMENT_RELAXED:-0}"
SETTLEMENT_WAS_WAIVED=0
export SETTLEMENT_RELAXED

status_json="$(curl -fsS --max-time 15 "$BASE/api/status")"
metrics_json="$(curl -fsS --max-time 15 "$BASE/api/metrics")"

settle_tmp="$(mktemp "${TMPDIR:-/tmp}/top_pool_settle.XXXXXX")"
settle_http="$(curl -sS --max-time 15 -o "$settle_tmp" -w "%{http_code}" "$BASE/api/worker/settlement" || true)"
if [[ "$settle_http" == "200" ]] && jq -e . <"$settle_tmp" >/dev/null 2>&1; then
  settle_json="$(cat "$settle_tmp")"
else
  if [[ "$SETTLEMENT_RELAXED" == "1" ]]; then
    settle_json='{"ok":true,"total_unpaid_hmc":0,"daily_sweep_eta_sec":0}'
    SETTLEMENT_WAS_WAIVED=1
    echo "[top-pool-gate] WARN: settlement unreachable or non-JSON (http=${settle_http}); SETTLEMENT_RELAXED=1 waives SLA checks" >&2
  else
    settle_json='{"ok":false}'
    echo "[top-pool-gate] WARN: settlement unreachable or non-JSON (http=${settle_http}); fix nginx allowlist or SETTLEMENT_RELAXED=1" >&2
  fi
fi
rm -f "$settle_tmp"
export SETTLEMENT_WAS_WAIVED

work_json="$(curl -fsS --max-time 15 "$COORD/api/work/stats?details=1")"

jq -nc \
  --argjson status "$status_json" \
  --argjson metrics "$metrics_json" \
  --argjson settle "$settle_json" \
  --argjson work "$work_json" \
  '{status:$status,metrics:$metrics,settlement:$settle,work:$work}' > "$OUT_DIR/raw.json"

python3 - "$SUMMARY" "$PROFILE" \
  "$MIN_WORKERS" "$MIN_BLOCKS_1H" "$MAX_OBS_BLOCK_SEC" "$MAX_UNPAID_HMC" "$MAX_SWEEP_ETA_SEC" "$MIN_PAYOUT_HMC" \
  "$status_json" "$metrics_json" "$settle_json" "$work_json" <<'PY'
import json
import os
import sys

summary_path = sys.argv[1]
profile = sys.argv[2]
min_workers = int(sys.argv[3])
min_blocks_1h = int(sys.argv[4])
max_obs_sec = float(sys.argv[5])
max_unpaid = float(sys.argv[6])
max_sweep_eta = int(sys.argv[7])
min_payout = float(sys.argv[8])

status = json.loads(sys.argv[9])
metrics = json.loads(sys.argv[10])
settle = json.loads(sys.argv[11])
work = json.loads(sys.argv[12])

# Public nginx may strip per-worker maps; fall back to workers_count.
_w = work.get("workers")
if isinstance(_w, dict):
    workers = len(_w)
elif isinstance(_w, list):
    workers = len(_w)
else:
    workers = int(work.get("workers_count") or 0)
blocks_1h = int(metrics.get("mining_poh_blocks_last_1h") or 0)
obs_sec = float(metrics.get("mining_observed_block_sec") or 0)
unpaid = float(settle.get("total_unpaid_hmc") or 0)
sweep_eta = int(settle.get("daily_sweep_eta_sec") or 0)
payout = float(work.get("total_payout_hmc") or 0)
mining = bool(status.get("mining"))
settle_ok = bool(settle.get("ok"))

checks = {
    "mining_on": mining,
    "settlement_api_ok": settle_ok,
    "workers_gte_min": workers >= min_workers,
    "blocks_1h_gte_min": blocks_1h >= min_blocks_1h,
    "observed_block_sec_lte_max": (obs_sec > 0 and obs_sec <= max_obs_sec),
    "unpaid_hmc_lte_max": unpaid <= max_unpaid,
    "sweep_eta_lte_max": sweep_eta <= max_sweep_eta,
    "total_payout_hmc_gte_min": payout >= min_payout,
}

relaxed = os.environ.get("SETTLEMENT_RELAXED", "0") == "1"
if relaxed and not settle_ok:
    checks["settlement_api_ok"] = True
    checks["unpaid_hmc_lte_max"] = True
    checks["sweep_eta_lte_max"] = True

failed = [k for k, v in checks.items() if not v]
passed = len(failed) == 0

settlement_waived = os.environ.get("SETTLEMENT_WAS_WAIVED", "0") == "1"

summary = {
    "gate": "top_pool_readiness_v1",
    "profile": profile,
    "pass": passed,
    "settlement_relaxed": settlement_waived,
    "failed_checks": failed,
    "thresholds": {
        "min_workers": min_workers,
        "min_blocks_1h": min_blocks_1h,
        "max_observed_block_sec": max_obs_sec,
        "max_unpaid_hmc": max_unpaid,
        "max_sweep_eta_sec": max_sweep_eta,
        "min_total_payout_hmc": min_payout,
    },
    "metrics": {
        "workers_count": workers,
        "blocks_last_1h": blocks_1h,
        "observed_block_sec": obs_sec,
        "total_unpaid_hmc": unpaid,
        "daily_sweep_eta_sec": sweep_eta,
        "total_payout_hmc": payout,
        "tip_height": int(status.get("tip_height") or 0),
        "mining": mining,
    },
    "checks": checks,
}

with open(summary_path, "w", encoding="utf-8") as f:
    json.dump(summary, f, indent=2)
print(json.dumps(summary, indent=2))
sys.exit(0 if passed else 1)
PY

echo "[top-pool-gate] summary: $SUMMARY"
