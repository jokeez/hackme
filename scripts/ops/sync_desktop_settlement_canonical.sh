#!/usr/bin/env bash
# Sync canonical settlement state from hackme.tech → local desktop worker_settlement_state.json.
# Run manually, from cron, or after VPS payout — keeps UI unpaid in sync with on-chain settles.
#
#   bash scripts/ops/sync_desktop_settlement_canonical.sh
#   */5 * * * * /path/to/HackMe/scripts/ops/sync_desktop_settlement_canonical.sh >>logs/sync-canonical.log 2>&1
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CANON_URL="${HACKME_SETTLEMENT_CANONICAL_URL:-https://hackme.tech/api/settlement/canonical.json}"
DEST="${HACKME_SETTLEMENT_CANONICAL_FILE:-$ROOT/logs/desktop/data/settlement_canonical_public.json}"
LOCAL_STATE="${HACKME_WORKER_SETTLEMENT_STATE_FILE:-$ROOT/logs/desktop/data/worker_settlement_state.json}"

mkdir -p "$(dirname "$DEST")" "$(dirname "$LOCAL_STATE")"
curl -fsS --max-time 25 -A 'HackMe-node-settlement/1' "$CANON_URL" -o "${DEST}.tmp"
mv "${DEST}.tmp" "$DEST"

python3 - "$DEST" "$LOCAL_STATE" <<'PY'
import json, sys

canon = json.loads(open(sys.argv[1]).read())
local_path = sys.argv[2]
try:
    local = json.loads(open(local_path).read())
except FileNotFoundError:
    local = {"workers": {}, "meta": {}}

workers = local.setdefault("workers", {})
changed = False
for wid, row in (canon.get("workers") or {}).items():
    cur = dict(workers.get(wid) or {})
    row_changed = False
    cs = float(row.get("settled_hmc") or 0)
    ls = float(cur.get("settled_hmc") or 0)
    if cs > ls:
        cur["settled_hmc"] = cs
        row_changed = True
    csup = float(row.get("settled_sup") or 0)
    lsup = float(cur.get("settled_sup") or 0)
    if csup > lsup:
        cur["settled_sup"] = csup
        row_changed = True
    for k in ("last_tx_hash", "last_settle_unix", "payout_address"):
        if row.get(k) and cur.get(k) != row.get(k):
            if k == "last_settle_unix":
                if int(row[k] or 0) > int(cur.get(k) or 0):
                    cur[k] = row[k]
                    row_changed = True
            else:
                cur[k] = row[k]
                row_changed = True
    if row_changed:
        workers[wid] = cur
        changed = True

meta = local.setdefault("meta", {})
clf = int((canon.get("meta") or {}).get("last_force_unix") or 0)
llf = int(meta.get("last_force_unix") or 0)
if clf > llf:
    meta["last_force_unix"] = clf
    changed = True

tmp = local_path + ".tmp"
json.dump(local, open(tmp, "w"), indent=2)
import os
os.replace(tmp, local_path)
print(f"[sync-canonical] merged into {local_path} (changed={changed})")
PY
