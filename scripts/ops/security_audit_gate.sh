#!/usr/bin/env bash
# Gate: POST /api/security-audit (order + fuzz campaign + report token).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_FILE="${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}"
WASM_HEX="${WASM_HEX:-$(xxd -p "$ROOT/tasks/artifacts/security/rust_script_push_bounds_guard.wasm" | tr -d '\n')}"

if [[ ! -f "$ADMIN_FILE" ]]; then
  echo "[audit-gate] missing admin token: $ADMIN_FILE" >&2
  exit 1
fi
ADMIN="$(tr -d '\r\n' <"$ADMIN_FILE")"

if ! curl -fsS --max-time 20 "$BASE/api/status" >/dev/null 2>&1; then
  echo "[audit-gate] node not up at $BASE — run: bash scripts/ops/desktop_mode_up.sh" >&2
  exit 1
fi

CID="audit-gate-$(date +%s)"
echo "[audit-gate] POST /api/security-audit $CID"
resp="$(curl -fsS -X POST "$BASE/api/security-audit" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "{\"title\":\"gate-audit\",\"payer_ref\":\"gate:audit\",\"budget_hmc\":0.5,\"budget_runs\":8,\"wasm_check_hex\":\"$WASM_HEX\",\"create_poh_order\":true}")"

echo "$resp" | python3 -c '
import json,sys
d=json.load(sys.stdin)
assert d.get("ok"), d
assert d.get("order_id"), d
assert d.get("campaign_id"), d
assert d.get("customer_report_token"), d
ps = d.get("pool_sync") or ""
warn = d.get("pool_sync_warning") or ""
print("ok order", d["order_id"], "campaign", d["campaign_id"], "pool_sync", ps, warn or "")
if ps not in ("ok", "queued"):
    raise SystemExit("unexpected pool_sync status: %r" % ps)
'

TOK="$(echo "$resp" | python3 -c 'import json,sys; print(json.load(sys.stdin)["customer_report_token"])')"
CID_OUT="$(echo "$resp" | python3 -c 'import json,sys; print(json.load(sys.stdin)["campaign_id"])')"

curl -fsS "$BASE/api/fuzz/campaigns/${CID_OUT}/report?format=json&limit=5" \
  -H "X-Hackme-Report-Token: $TOK" | python3 -c '
import json,sys
d=json.load(sys.stdin)
ver = d.get("report_version","")
assert ver == "fuzz_report_v2", ver
issues = d.get("top_issues") or []
for it in issues:
    if it.get("repro_cmd"):
        assert "check_repro" in it["repro_cmd"], it["repro_cmd"]
    if it.get("triage_class"):
        assert it["triage_class"] in ("expected_signal","needs_triage","sandbox","review"), it
print("report verdict", d.get("verdict","?"), "version", ver, "issues", len(issues))
'

html="$(curl -fsS "$BASE/api/fuzz/campaigns/${CID_OUT}/report?format=html&limit=5" \
  -H "X-Hackme-Report-Token: $TOK")"
echo "$html" | grep -q 'fuzz_report_v2' || { echo "[audit-gate] HTML missing fuzz_report_v2" >&2; exit 1; }
echo "$html" | grep -q 'Scope &amp; honesty' || { echo "[audit-gate] HTML missing scope block" >&2; exit 1; }

echo "[audit-gate] PASS"
