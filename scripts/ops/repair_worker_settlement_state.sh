#!/usr/bin/env bash
# Clamp worker_settlement_state.json settled_hmc so it never exceeds coordinator payout_hmc.
# Over-counted state makes settle_worker_payouts.sh skip payouts (delta=0).
#
#   NODE_SSH=hackme-vps bash scripts/ops/repair_worker_settlement_state.sh
#   DRY_RUN=1 NODE_SSH=hackme-vps bash scripts/ops/repair_worker_settlement_state.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
DRY_RUN="${DRY_RUN:-0}"

if [[ -n "$NODE_SSH" ]]; then
  DRY_RUN="$DRY_RUN" DEPLOY="$DEPLOY" \
    ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" \
    "bash '$DEPLOY/scripts/ops/repair_worker_settlement_state.sh'" 2>/dev/null || \
    ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" \
    "DRY_RUN='$DRY_RUN' DEPLOY='$DEPLOY' bash -s" <"$ROOT/scripts/ops/repair_worker_settlement_state.sh"
  exit $?
fi

export DEPLOY
export DRY_RUN
python3 <<'PY'
import json, os, sys, urllib.request

deploy = os.environ.get("DEPLOY", "/opt/hackme")
dry = os.environ.get("DRY_RUN", "0") == "1"
state_path = os.environ.get("STATE_FILE", f"{deploy}/data/worker_settlement_state.json")
coord_url = os.environ.get("COORD_URL", "http://127.0.0.1:18081")
secret = f"{deploy}/.secrets/hackme_coordinator_admin_token"
token = os.environ.get("COORD_ADMIN_TOKEN", "")
if not token and os.path.isfile(secret):
    token = open(secret).read().strip()

if not token:
    print("[repair-settle-state] COORD_ADMIN_TOKEN required", file=sys.stderr)
    sys.exit(1)

req = urllib.request.Request(
    f"{coord_url}/api/work/stats?details=1",
    headers={"X-Hackme-Admin-Token": token},
)
stats = json.loads(urllib.request.urlopen(req, timeout=20).read())
workers = stats.get("workers") or {}
if not workers:
    print("[repair-settle-state] no workers{} in coordinator stats — nothing to repair")
    sys.exit(0)

st = json.load(open(state_path))
changed = False
for wid, row in workers.items():
    payout = float(row.get("payout_hmc") or 0)
    wst = st.setdefault("workers", {}).setdefault(wid, {})
    settled = float(wst.get("settled_hmc") or 0)
    if settled > payout + 1e-9:
        print(f"[repair-settle-state] {wid}: settled_hmc {settled:.12f} -> {payout:.12f} (payout_hmc)")
        if not dry:
            wst["settled_hmc"] = payout
        changed = True

if changed and not dry:
    json.dump(st, open(state_path, "w"), indent=2)
    print("[repair-settle-state] state file updated")
elif not changed:
    print("[repair-settle-state] no repairs needed")
PY
