#!/usr/bin/env bash
# Demo: user RPG C++ -> from_code + fuzz report (what actually happens).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
SRC="${1:-$ROOT/logs/desktop/user_rpg_abyss_full.cpp}"
OUT_DIR="$ROOT/logs/desktop/user_rpg_demo_$(date +%Y%m%dT%H%M%S)"
mkdir -p "$OUT_DIR"
ADMIN_TOKEN="${ADMIN_TOKEN:-$(grep '^HACKME_ADMIN_TOKEN=' .env.desktop | cut -d= -f2-)}"
BASE="${BASE:-http://127.0.0.1:8080}"
if [[ ! -f "$SRC" ]]; then echo "missing source: $SRC" >&2; exit 2; fi

python3 - "$SRC" "$OUT_DIR" "$ADMIN_TOKEN" "$BASE" <<'PY'
import json, sys, urllib.request

src_path, out_dir, token, base = sys.argv[1:5]
code = open(src_path, encoding="utf-8").read()

def post(path, body):
    req = urllib.request.Request(
        base + path,
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", "X-Hackme-Admin-Token": token},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            return r.status, json.loads(r.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read())

def get(path):
    req = urllib.request.Request(base + path, headers={"X-Hackme-Admin-Token": token})
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.loads(r.read())

# 1) from_code — full RPG
fc_body = {
    "id": "order-rpg-abyss-full",
    "language": "cpp",
    "code": code,
    "reward_hmc": 1.0,
    "difficulty_score": 10,
    "target_solves": 1,
    "payer_ref": "demo:rpg-upload",
}
st, fc = post("/api/tasks/from_code", fc_body)
open(f"{out_dir}/01_from_code_full.json", "w").write(json.dumps(fc, indent=2, ensure_ascii=False))
print(f"[1] POST /api/tasks/from_code (full RPG): HTTP {st} code={fc.get('code', 'ok')}")

# 2) fuzz campaign (generic worker — no WASM from RPG)
cid = f"campaign-rpg-abyss-{int(__import__('time').time())}"
st2, cr = post("/api/tasks/from_code" if False else "/api/fuzz/campaigns", {
    "id": cid,
    "campaign_type": "fuzz",
    "status": "running",
    "title": "RPG Abyss upload (no WASM target)",
    "description": "Full console RPG cannot compile to check(); autorunner uses default property check",
    "owner_ref": "user:rpg-cpp",
    "budget_runs": 64,
    "budget_seconds": 120,
    "config": {"auto_runner": "1", "worker_batch": 16, "queue_depth": 64},
})
# fix: fuzz create
import urllib.error
req = urllib.request.Request(
    base + "/api/fuzz/campaigns",
    data=json.dumps({
        "id": cid,
        "campaign_type": "fuzz",
        "status": "running",
        "title": "RPG Abyss — fuzz without WASM",
        "owner_ref": "user:rpg-cpp",
        "budget_runs": 64,
        "config": {"worker_batch": 16},
    }).encode(),
    headers={"Content-Type": "application/json", "X-Hackme-Admin-Token": token},
    method="POST",
)
try:
    with urllib.request.urlopen(req, timeout=30) as r:
        cr = json.loads(r.read())
        st2 = r.status
except urllib.error.HTTPError as e:
    st2, cr = e.code, json.loads(e.read())
open(f"{out_dir}/02_fuzz_create.json", "w").write(json.dumps(cr, indent=2))
print(f"[2] fuzz campaign create: HTTP {st2} id={cid}")

# wait for autorunner
import time
time.sleep(12)
report = get(f"/api/fuzz/campaigns/{cid}/report?limit=20")
open(f"{out_dir}/03_fuzz_report.json", "w").write(json.dumps(report, indent=2, ensure_ascii=False))
print(f"[3] fuzz report verdict={report.get('verdict')} findings={report.get('totals',{}).get('findings_total')}")
open(f"{out_dir}/campaign_id.txt", "w").write(cid)
PY

echo "Artifacts: $OUT_DIR"
jq '{code,error,compile_log:(.compile_log|if type=="string" then .[0:800] else . end)}' "$OUT_DIR/01_from_code_full.json" 2>/dev/null || true
jq '{verdict,security_summary,top_issues,recommendations}' "$OUT_DIR/03_fuzz_report.json" 2>/dev/null || true
