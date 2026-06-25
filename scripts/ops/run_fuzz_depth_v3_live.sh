#!/usr/bin/env bash
# Fuzz Depth v3 live demo — wasm_native tier + native bridge on bitcoin dup-inputs guard.
#
#   bash scripts/ops/run_fuzz_depth_v3_live.sh
#   DEPTH_TIER=bytes_corpus BUDGET_RUNS=128 bash scripts/ops/run_fuzz_depth_v3_live.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN="$(tr -d '\r\n' <"${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}" 2>/dev/null || true)"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/fuzz-depth-v3/$STAMP}"
DEPTH_TIER="${DEPTH_TIER:-wasm_native}"
BUDGET_RUNS="${BUDGET_RUNS:-64}"
BUDGET_HMC="${BUDGET_HMC:-5}"
WAIT_SEC="${WAIT_SEC:-90}"
POOL_DIST="${POOL_DIST:-0}"

require_cmd curl jq xxd python3

[[ -n "$ADMIN" ]] || fail "missing admin token at .secrets/hackme_admin_token"
curl -fsS --max-time 10 "${BASE}/api/status?lite=1" >/dev/null || fail "node down at $BASE — bash scripts/ops/desktop_mode_up.sh"

mkdir -p "$OUT"
log() { echo "[fuzz-v3] $*" | tee -a "$OUT/run.log"; }

log "build upstream WASM"
bash "$ROOT/scripts/build_upstream_l1_pack.sh" >>"$OUT/build.log" 2>&1
WASM="$ROOT/tasks/artifacts/security/upstream_bitcoin_tx_dup_inputs.wasm"
[[ -f "$WASM" ]] || fail "missing $WASM"
WASM_HEX="$(xxd -p "$WASM" | tr -d '\n')"
CID="campaign-fuzz-v3-${STAMP}"
OID="order-fuzz-v3-${STAMP}"

log "POST security-audit depth_tier=$DEPTH_TIER runs=$BUDGET_RUNS hmc=$BUDGET_HMC"
resp="$(curl -fsS --max-time 120 -X POST "${BASE}/api/security-audit" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "$(jq -nc \
    --arg title "Fuzz Depth v3 · duplicate inputs + native bridge" \
    --arg payer "research:fuzz-depth-v3" \
    --arg oid "$OID" \
    --arg cid "$CID" \
    --arg hex "$WASM_HEX" \
    --arg tier "$DEPTH_TIER" \
    --argjson runs "$BUDGET_RUNS" \
    --argjson hmc "$BUDGET_HMC" \
    --arg pool "$POOL_DIST" \
    '{
      title: $title, payer_ref: $payer, order_id: $oid, campaign_id: $cid,
      wasm_check_hex: $hex, budget_hmc: $hmc, budget_runs: $runs,
      depth_tier: $tier, guard_name: "bitcoin_tx_dup_inputs",
      create_poh_order: true, pool_distributed: ($pool == "1")
    }')")"
echo "$resp" | jq . >"$OUT/audit.json"

TOK="$(jq -r '.customer_report_token' <<<"$resp")"
CID_OUT="$(jq -r '.campaign_id' <<<"$resp")"
[[ -n "$TOK" && "$TOK" != null ]] || fail "no report token"

log "wait ${WAIT_SEC}s for autorunner + native bridge"
sleep "$WAIT_SEC"

log "fetch report JSON"
curl -fsS "${BASE}/api/fuzz/campaigns/${CID_OUT}/report?format=json&limit=50" \
  -H "X-Hackme-Report-Token: $TOK" | jq . >"$OUT/report.json"

log "fetch escrow"
curl -fsS "${BASE}/api/fuzz/campaigns/${CID_OUT}/escrow" 2>/dev/null | jq . >"$OUT/escrow.json" || true

python3 - "$OUT" <<'PY'
import json, sys
from pathlib import Path
out = Path(sys.argv[1])
rep = json.loads((out / "report.json").read_text())
summary = rep.get("security_summary") or {}
native = (rep.get("campaign") or {}).get("summary", {}).get("native") or {}
findings = rep.get("findings") or []
rollup = {
    "campaign_id": (rep.get("campaign") or {}).get("id"),
    "depth_tier": (rep.get("campaign") or {}).get("config", {}).get("depth_tier"),
    "runs_done": summary.get("runs_done"),
    "guard_signals": summary.get("vulnerabilities_found"),
    "critical": summary.get("critical_count"),
    "native_status": native.get("status"),
    "native_confirmed": native.get("confirmed_count", 0),
    "native_rejected": native.get("rejected_count", 0),
    "verdict": rep.get("verdict"),
    "finding_count": len(findings),
}
(out / "DAY_SUMMARY.json").write_text(json.dumps(rollup, indent=2) + "\n")
print(json.dumps(rollup, indent=2))
PY

ln -sfn "$OUT" "$ROOT/reports/fuzz-depth-v3/CURRENT"
log "done → $OUT/DAY_SUMMARY.json"
