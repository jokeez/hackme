#!/usr/bin/env bash
# Autorun fuzz campaign with realistic property-check findings (not quarantine spam).
#
# Mode:
#   MODE=bounds   — rust_bounds_guard.wasm (default): many check()==0 inputs → property_violation
#   MODE=oob      — legacy OOB-trap hex (sandbox quarantine noise — avoid for demos)
#
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN="$(head -n1 "${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}" | tr -d '\r\n')"
MODE="${MODE:-bounds}"
CID="${CID:-campaign-redteam-demo-$(date +%s)}"
TITLE="${TITLE:-redteam-property-fuzz}"
RUNS="${RUNS:-96}"
WAIT_SEC="${WAIT_SEC:-20}"

OOB_WASM_HEX="0061736d0100000001060160017e017f03020100050301000107090105636865636b00000a0e010c00418080042802001a41010b"
BOUNDS_WASM="$ROOT/tasks/artifacts/security/rust_bounds_guard.wasm"

if [[ -z "$ADMIN" ]]; then
  echo "[redteam-demo] missing admin token" >&2
  exit 1
fi
if [[ "$MODE" == "bounds" ]]; then
  [[ -f "$BOUNDS_WASM" ]] || { echo "[redteam-demo] build guards first: bash scripts/build_security_task_pack.sh" >&2; exit 1; }
  WASM_HEX="$(xxd -ps "$BOUNDS_WASM" | tr -d '\n')"
  TITLE="${TITLE:-redteam-bounds-property}"
  echo "[redteam-demo] mode=bounds ($BOUNDS_WASM)"
elif [[ "$MODE" == "oob" ]]; then
  WASM_HEX="$OOB_WASM_HEX"
  TITLE="${TITLE:-redteam-oob-trap}"
  echo "[redteam-demo] mode=oob (legacy — expect sandbox quarantine duplicates; use MODE=bounds)"
else
  echo "[redteam-demo] unknown MODE=$MODE (bounds|oob)" >&2
  exit 1
fi

hdr=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json")

echo "[redteam-demo] create $CID"
curl -fsS -X POST "$BASE/api/fuzz/campaigns" "${hdr[@]}" -d "$(jq -nc \
  --arg id "$CID" --arg title "$TITLE" --arg hex "$WASM_HEX" --argjson runs "$RUNS" \
  '{id:$id,campaign_type:"fuzz",status:"running",title:$title,
    description:"Autorunner property fuzz — check() fails on most random inputs",
    owner_ref:"hackme:redteam",budget_runs:$runs,budget_seconds:120,
    config:{auto_runner:"1",worker_batch:24,queue_depth:128,wasm_check_hex:$hex}}')" | jq -c '{ok,id:.campaign.id,status:.campaign.status}'

echo "[redteam-demo] wait ${WAIT_SEC}s…"
sleep "$WAIT_SEC"

echo "[redteam-demo] report"
curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" "$BASE/api/fuzz/campaigns/$CID/report?limit=15&format=json" \
  | jq -c '{verdict,security_summary,top_issues:[.top_issues[]?|{severity,finding_type,title}],totals}'

echo "[redteam-demo] UI: Fuzz → Refresh → $TITLE → Open report"
echo "  campaign_id=$CID"
