#!/usr/bin/env bash
# Capture coordinator + wallet + campaign distribution snapshot (bootstrap audit bot).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-https://hackme.tech/pool/coordinator}"
STAMP="${1:-$(date -u +%Y%m%dT%H%M%SZ)}"
LABEL="${2:-snapshot}"
OUT="${SNAP_DIR:-$INSTALL/logs/bootstrap/snapshots}/${STAMP}-${LABEL}"
mkdir -p "$OUT"

ADMIN="$(tr -d '\r\n' <"${ADMIN_FILE:-$INSTALL/.env}" 2>/dev/null | sed -n 's/^HACKME_ADMIN_TOKEN=//p' | head -1)"
[[ -z "$ADMIN" && -f "$INSTALL/.env" ]] && ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$INSTALL/.env" | cut -d= -f2- | tr -d '\r\n')"
COORD_ADMIN="$(tr -d '\r\n' <"${COORD_ADMIN_FILE:-$INSTALL/.secrets/coordinator_admin.token}" 2>/dev/null || true)"

log() { echo "[snapshot] $*" | tee -a "$OUT/run.log"; }
log "stamp=$STAMP label=$LABEL out=$OUT"

curl -fsS --max-time 20 "$BASE/api/status?lite=1" 2>/dev/null | jq . >"$OUT/node_status.json" || echo '{}' >"$OUT/node_status.json"
curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $ADMIN" "$BASE/api/wallet" 2>/dev/null | jq . >"$OUT/wallet.json" || echo '{}' >"$OUT/wallet.json"
curl -fsS --max-time 30 "$COORD/api/fuzz/pool/stats" 2>/dev/null | jq . >"$OUT/pool_fuzz_stats.json" || echo '{}' >"$OUT/pool_fuzz_stats.json"

if [[ -n "$COORD_ADMIN" ]]; then
  curl -fsS --max-time 30 -H "X-Hackme-Admin-Token: $COORD_ADMIN" \
    "$COORD/api/work/stats?details=1" 2>/dev/null | jq . >"$OUT/coordinator_workers.json" || true
fi

if [[ -n "${CAMPAIGN_ID:-}" ]]; then
  curl -fsS --max-time 30 "$COORD/api/fuzz/pool/campaigns/progress?id=${CAMPAIGN_ID}" 2>/dev/null \
    | jq . >"$OUT/campaign_progress.json" || true
  curl -fsS --max-time 30 -H "X-Hackme-Admin-Token: $ADMIN" \
    "$BASE/api/fuzz/campaigns/${CAMPAIGN_ID}/escrow" 2>/dev/null | jq . >"$OUT/campaign_escrow.json" || true
fi

python3 - "$OUT" <<'PY'
import json, pathlib, sys
out = pathlib.Path(sys.argv[1])
w = json.loads((out / "wallet.json").read_text()) if (out / "wallet.json").exists() else {}
pool = json.loads((out / "pool_fuzz_stats.json").read_text()) if (out / "pool_fuzz_stats.json").exists() else {}
workers = json.loads((out / "coordinator_workers.json").read_text()) if (out / "coordinator_workers.json").exists() else {}
rigs = workers.get("active_rigs") or workers.get("workers") or []
top = sorted(rigs, key=lambda x: float(x.get("hashrate_gh_s") or 0), reverse=True)[:8]
summary = {
    "wallet_hmc": w.get("balance_hmc"),
    "wallet_address": w.get("address"),
    "pool_work_done": pool.get("work_done"),
    "pool_campaigns_running": pool.get("campaigns_running"),
    "fleet_ghs_top": [{"worker_id": r.get("worker_id"), "ghs": r.get("hashrate_gh_s")} for r in top],
    "total_payout_hmc": workers.get("total_payout_hmc"),
}
(out / "SUMMARY.json").write_text(json.dumps(summary, indent=2) + "\n")
print(json.dumps(summary, indent=2))
PY

log "done → $OUT/SUMMARY.json"
echo "$OUT"
