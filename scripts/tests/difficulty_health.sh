#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq
require_cmd python3

BASE="${BASE:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/difficulty_health"
ensure_reports_dir "$OUT"

TARGET_SEC="${TARGET_SEC:-30}"
MIN_TARGET_MOD="${MIN_TARGET_MOD:-251}"
MAX_TARGET_MOD="${MAX_TARGET_MOD:-10000000000000}"
MAX_FAST_RATIO="${MAX_FAST_RATIO:-0.25}"
MAX_SLOW_RATIO="${MAX_SLOW_RATIO:-4.0}"
ALLOW_TARGET_CAP="${ALLOW_TARGET_CAP:-0}"
# Public HTTPS + busy canonical miner: large chain snapshots can stall mid-body (~20KiB) waiting on DB;
# 10 PoH rows is enough for difficulty_health sampling and completes reliably against hackme.tech/pool.
CHAIN_LIMIT="${CHAIN_LIMIT:-10}"
METRICS_TIMEOUT="${METRICS_TIMEOUT:-45}"
CHAIN_TIMEOUT="${CHAIN_TIMEOUT:-45}"

metrics_json="$(curl_retry_fsS -fsS --max-time "${METRICS_TIMEOUT}" "$BASE/api/metrics")"
printf '%s\n' "$metrics_json" >"$OUT/metrics.json"

chain_json="$(curl_retry_fsS -fsS --max-time "${CHAIN_TIMEOUT}" "$BASE/api/chain?limit=${CHAIN_LIMIT}")"
printf '%s\n' "$chain_json" >"$OUT/chain.json"

python3 - "$OUT/metrics.json" "$OUT/chain.json" "$OUT/summary.json" \
  "$TARGET_SEC" "$MIN_TARGET_MOD" "$MAX_TARGET_MOD" "$MAX_FAST_RATIO" "$MAX_SLOW_RATIO" "$ALLOW_TARGET_CAP" <<'PY'
import base64
import json
import math
import sys

metrics_path, chain_path, summary_path = sys.argv[1], sys.argv[2], sys.argv[3]
target_sec = float(sys.argv[4])
min_mod = int(sys.argv[5])
max_mod = int(sys.argv[6])
max_fast_ratio = float(sys.argv[7])
max_slow_ratio = float(sys.argv[8])
allow_target_cap = str(sys.argv[9]).strip() == "1"

with open(metrics_path, "r", encoding="utf-8") as f:
    m = json.load(f)
with open(chain_path, "r", encoding="utf-8") as f:
    c = json.load(f)

reasons = []
warnings = []

mod = int(m.get("mining_target_mod") or 0)
mod_cap = int(m.get("mining_target_mod_cap") or 0)
mod_at_cap = bool(m.get("mining_target_mod_at_cap"))
obs = float(m.get("mining_observed_block_sec") or -1)
target_from_metrics = float(m.get("mining_target_block_sec") or target_sec)
blocks_1h = int(m.get("mining_poh_blocks_last_1h") or 0)

if mod < min_mod or mod > max_mod:
    reasons.append(f"target_mod_out_of_bounds:{mod}")
if mod_cap and mod > mod_cap:
    reasons.append(f"target_mod_above_reported_cap:{mod}>{mod_cap}")
if mod_at_cap and not allow_target_cap:
    reasons.append("target_mod_at_cap")

target_ref = target_from_metrics if target_from_metrics > 0 else target_sec
if obs > 0 and blocks_1h >= 5:
    ratio = obs / target_ref
    if ratio < max_fast_ratio:
        reasons.append(f"block_rate_too_fast:ratio={ratio:.4f}")
    if ratio > max_slow_ratio:
        reasons.append(f"block_rate_too_slow:ratio={ratio:.4f}")
elif obs <= 0 and blocks_1h >= 5:
    warnings.append("observed_block_sec_unavailable")

blocks = c.get("blocks") or []
poh_rows = []
for b in blocks:
    task = (b or {}).get("task") or {}
    if str(task.get("kind") or "") != "synthetic_poh_v1":
        continue
    idx = int((b or {}).get("index") or 0)
    ts = int((b or {}).get("timestamp_unix") or 0)
    payload = str(task.get("payload") or "")
    payload_mod = None
    if payload:
        try:
            raw = base64.b64decode(payload)
            p = json.loads(raw.decode("utf-8", errors="ignore"))
            if isinstance(p, dict) and p.get("target_mod") is not None:
                payload_mod = int(p.get("target_mod"))
        except Exception:
            warnings.append(f"payload_decode_failed:index={idx}")
    poh_rows.append((idx, ts, payload_mod))

poh_rows.sort(key=lambda x: x[0])
prev_ts = None
for idx, ts, payload_mod in poh_rows:
    if prev_ts is not None and ts < prev_ts:
        reasons.append(f"timestamp_regression:index={idx}")
    prev_ts = ts
    if payload_mod is not None and (payload_mod < min_mod or payload_mod > max_mod):
        reasons.append(f"payload_target_mod_out_of_bounds:index={idx},m={payload_mod}")

status = "PASS" if not reasons else "FAIL"
summary = {
    "status": status,
    "target_mod": mod,
    "target_mod_cap": mod_cap,
    "target_mod_at_cap": mod_at_cap,
    "target_block_sec": target_ref,
    "observed_block_sec": obs,
    "poh_blocks_last_1h": blocks_1h,
    "poh_rows_sampled": len(poh_rows),
    "reasons": reasons,
    "warnings": sorted(set(warnings)),
}
with open(summary_path, "w", encoding="utf-8") as f:
    json.dump(summary, f, ensure_ascii=False)

print(json.dumps(summary, ensure_ascii=False))
if reasons:
    sys.exit(1)
PY

pass "difficulty health PASS. See $OUT"
