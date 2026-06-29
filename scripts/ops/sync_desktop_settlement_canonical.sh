#!/usr/bin/env bash
# Sync canonical settlement state from hackme.tech to local desktop node (UI unpaid display).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CANON_URL="${HACKME_SETTLEMENT_CANONICAL_URL:-https://hackme.tech/api/settlement/canonical.json}"
DEST="${HACKME_WORKER_SETTLEMENT_CANONICAL_FILE:-$ROOT/logs/desktop/data/settlement_canonical_public.json}"
LOCAL_STATE="${HACKME_WORKER_SETTLEMENT_STATE_FILE:-$ROOT/logs/desktop/data/worker_settlement_state.json}"

mkdir -p "$(dirname "$DEST")"
curl -fsS --max-time 25 "$CANON_URL" -o "${DEST}.tmp"
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
for wid, row in (canon.get("workers") or {}).items():
    cur = workers.setdefault(wid, {})
    cs = float(row.get("settled_hmc") or 0)
    ls = float(cur.get("settled_hmc") or 0)
    if cs > ls:
        cur["settled_hmc"] = cs
    csup = float(row.get("settled_sup") or 0)
    lsup = float(cur.get("settled_sup") or 0)
    if csup > lsup:
        cur["settled_sup"] = csup
    for k in ("last_tx_hash", "last_settle_unix", "payout_address"):
        if row.get(k):
            cur[k] = row[k]
    workers[wid] = cur
meta = local.setdefault("meta", {})
if int(canon.get("meta", {}).get("last_force_unix") or 0) > int(meta.get("last_force_unix") or 0):
    meta["last_force_unix"] = canon["meta"]["last_force_unix"]
json.dump(local, open(local_path, "w"), indent=2)
print(f"[sync-canonical] merged into {local_path}")
PY
