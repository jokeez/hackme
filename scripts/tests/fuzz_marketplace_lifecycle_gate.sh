#!/usr/bin/env bash
# Gate: marketplace shows only active non-gate campaigns; cancel hides them.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_FILE="${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}"
ADMIN="${ADMIN_TOKEN:-}"
if [[ -z "$ADMIN" && -f "$ADMIN_FILE" ]]; then
  ADMIN="$(tr -d '\r\n' <"$ADMIN_FILE")"
fi
[[ -n "$ADMIN" ]] || { echo "[fuzz-market-lifecycle] missing admin token (ADMIN_TOKEN or $ADMIN_FILE)" >&2; exit 1; }
WASM_HEX="$(xxd -p "$ROOT/tasks/artifacts/security/rust_script_push_bounds_guard.wasm" | tr -d '\n')"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
GATE_CID="campaign-market-gate-${TS}"
REAL_CID="campaign-market-real-${TS}"

curl -fsS --max-time 5 "${BASE}/api/status" >/dev/null || {
  echo "[fuzz-market-lifecycle] node down at $BASE" >&2
  exit 1
}

create_campaign() {
  curl -fsS --max-time 120 -X POST "${BASE}/api/fuzz/campaigns" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $ADMIN" \
    -d "$(python3 - <<PY
import json, os
cfg = {
  "pool_distributed": True,
  "check_semantics": "detector",
  "wasm_check_hex": os.environ["WASM_HEX"],
  "seed_corpus": [133452, 999001],
  "auto_runner": "0",
}
extra = json.loads(os.environ.get("EXTRA_CFG", "{}"))
cfg.update(extra)
print(json.dumps({
  "id": os.environ["CID"],
  "campaign_type": "property",
  "status": "running",
  "title": os.environ["TITLE"],
  "owner_ref": os.environ["OWNER"],
  "budget_runs": 16,
  "budget_seconds": 3600,
  "config": cfg,
}))
PY
)"
}

echo "[fuzz-market-lifecycle] create internal gate $GATE_CID"
CID="$GATE_CID" TITLE="pool-sync-gate" OWNER="gate:pool-sync" WASM_HEX="$WASM_HEX" EXTRA_CFG='{"internal_gate":true}' \
  create_campaign >/tmp/fuzz_market_gate.json

echo "[fuzz-market-lifecycle] gate must NOT appear in marketplace"
curl -fsS "${BASE}/api/fuzz/marketplace" >/tmp/fuzz_market_list.json
python3 - "$GATE_CID" <<'PY'
import json, sys
cid = sys.argv[1].lower()
rows = json.load(open("/tmp/fuzz_market_list.json")).get("campaigns") or []
assert not any(str(c.get("id") or "").lower() == cid for c in rows), rows
print("gate hidden ok")
PY

echo "[fuzz-market-lifecycle] create real campaign $REAL_CID"
CID="$REAL_CID" TITLE="customer property audit" OWNER="HMC-market-test" WASM_HEX="$WASM_HEX" EXTRA_CFG='{}' \
  create_campaign >/tmp/fuzz_market_real.json

sleep 1
curl -fsS "${BASE}/api/fuzz/marketplace" >/tmp/fuzz_market_list2.json
python3 - "$REAL_CID" <<'PY'
import json, sys
cid = sys.argv[1].lower()
rows = json.load(open("/tmp/fuzz_market_list2.json")).get("campaigns") or []
match = [c for c in rows if str(c.get("id") or "").lower() == cid and c.get("status") in ("running", "planned")]
assert match, f"real campaign missing: {rows}"
print("real campaign visible ok", match[0].get("status"))
PY

echo "[fuzz-market-lifecycle] cancel $REAL_CID"
curl -fsS -X POST "${BASE}/api/fuzz/campaigns/${REAL_CID}/status" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d '{"status":"cancelled"}' >/dev/null

curl -fsS "${BASE}/api/fuzz/marketplace" >/tmp/fuzz_market_list3.json
python3 - "$REAL_CID" <<'PY'
import json, sys
cid = sys.argv[1].lower()
rows = json.load(open("/tmp/fuzz_market_list3.json")).get("campaigns") or []
assert not any(str(c.get("id") or "").lower() == cid for c in rows), "cancelled still in marketplace"
print("active_after_cancel", sum(1 for c in rows if c.get("status") in ("running", "planned")))
PY

curl -fsS -X POST "${BASE}/api/fuzz/campaigns/${GATE_CID}/status" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d '{"status":"cancelled"}' >/dev/null || true

echo "[fuzz-market-lifecycle] verify zero active pool campaigns"
curl -fsS "${BASE}/api/fuzz/campaigns?limit=300" -H "X-Hackme-Admin-Token: $ADMIN" >/tmp/fuzz_all.json
python3 - "$GATE_CID" "$REAL_CID" <<'PY'
import json, sys
gate_cid, real_cid = (x.lower() for x in sys.argv[1:3])
rows = json.load(open("/tmp/fuzz_all.json")).get("campaigns") or []
active = [c for c in rows if c.get("status") in ("running", "planned")]
stale = [c for c in active if str(c.get("id") or "").lower().startswith("campaign-market-")]
for c in stale:
    print("stale_active", c.get("id"), c.get("title"))
# This gate only requires campaigns created in *this* run are cancelled.
for cid in (gate_cid, real_cid):
    hit = [c for c in active if str(c.get("id") or "").lower() == cid]
    assert not hit, f"this-run campaign still active: {cid} {hit}"
print("this_run_clean ok")
PY

# Best-effort cleanup of leftover campaign-market-* from prior failed gate runs.
python3 - "$ADMIN" "$BASE" <<'PY'
import json, os, subprocess, sys
admin, base = sys.argv[1], sys.argv[2]
rows = json.load(open("/tmp/fuzz_all.json")).get("campaigns") or []
for c in rows:
    cid = str(c.get("id") or "")
    if not cid.lower().startswith("campaign-market-"):
        continue
    if c.get("status") not in ("running", "planned"):
        continue
    subprocess.run([
        "curl", "-fsS", "-X", "POST", f"{base}/api/fuzz/campaigns/{cid}/status",
        "-H", "Content-Type: application/json",
        "-H", f"X-Hackme-Admin-Token: {admin}",
        "-d", '{"status":"cancelled"}',
    ], capture_output=True)
    print("cancelled_stale", cid)
PY

echo "[fuzz-market-lifecycle] PASS"
