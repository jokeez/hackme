#!/usr/bin/env bash
# Snapshot bootstrap wallet + recent pool campaign completions (payout track).
# Timer: every 6h alongside / after bot runs.
set -euo pipefail
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD_FUZZ_HINT="${COORD_FUZZ_HINT:-https://hackme.tech}"
LOG="$INSTALL/logs/bootstrap/payout_track.jsonl"
mkdir -p "$INSTALL/logs/bootstrap"

ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$INSTALL/.env" | cut -d= -f2- | tr -d '\r\n')"
wallet="$(curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $ADMIN" "$BASE/api/wallet" || echo '{}')"

python3 - "$INSTALL" "$LOG" "$wallet" <<'PY'
import json, sqlite3, sys, time, pathlib, urllib.request
install, log_path, wallet_raw = sys.argv[1:4]
try:
    wallet = json.loads(wallet_raw or "{}")
except Exception:
    wallet = {}
row = {
    "ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "event": "payout_snapshot",
    "spendable_hmc": float(wallet.get("balance_orders_spendable_hmc") or wallet.get("balance_hmc") or 0),
    "balance_hmc": float(wallet.get("balance_hmc") or 0),
    "address": (wallet.get("address") or "")[:32],
}
db = pathlib.Path(install) / "data" / "hackme.db"
camps = []
if db.is_file():
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True, timeout=5)
    for r in con.execute(
        "SELECT id,status,budget_runs,COALESCE(owner_ref,''),summary_json,created_at "
        "FROM fuzz_campaigns WHERE created_at > strftime('%s','now','-4 days') "
        "ORDER BY created_at DESC LIMIT 20"
    ):
        cid, st, br, owner, summary_json, created = r
        summary = {}
        try:
            summary = json.loads(summary_json or "{}")
        except Exception:
            pass
        camps.append({
            "id": cid,
            "status": st,
            "budget_runs": br,
            "runs_done": summary.get("runs_done"),
            "owner_ref": owner,
            "created_at": created,
        })
    con.close()
row["campaigns_4d"] = camps
# Public marketplace peek (hub) — best effort
try:
    with urllib.request.urlopen("https://hackme.tech/api/fuzz/marketplace", timeout=12) as r:
        data = json.load(r)
    pub = []
    for c in (data.get("campaigns") or [])[:15]:
        if "bootstrap" in str(c.get("id") or "") or "bootstrap" in str(c.get("title") or "").lower():
            pub.append({k: c.get(k) for k in ("id", "status", "runs_done", "budget_runs", "title")})
    row["hub_bootstrap_visible"] = pub
except Exception as e:
    row["hub_marketplace_err"] = str(e)[:120]

pathlib.Path(log_path).parent.mkdir(parents=True, exist_ok=True)
with open(log_path, "a") as f:
    f.write(json.dumps(row, ensure_ascii=False) + "\n")
print(f"[payout-track] spendable={row['spendable_hmc']} camps_4d={len(camps)}")
PY
