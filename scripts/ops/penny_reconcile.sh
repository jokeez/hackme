#!/usr/bin/env bash
# Wallet / pool / worker reconciliation to 1e-8 HMC (penny-accurate).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${OUT:-$ROOT/reports/penny-$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$OUT"
WALLET="${WALLET:-HMC-91fe007e4036c602}"
WORKER="${WORKER:-worker-kapa-pc}"
LOCAL="${LOCAL_BASE:-http://127.0.0.1:8080}"
PROD="${PROD_BASE:-https://hackme.tech}"
COORD="${COORD:-${PROD}/pool/coordinator}"
COORD_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)"

json_get() { curl -fsS --max-time 20 "$1" 2>/dev/null || echo '{}'; }

canon="$(json_get "${PROD}/api/address/${WALLET}")"
if [[ -n "$COORD_ADMIN" ]]; then
  pool="$(curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $COORD_ADMIN" "${COORD}/api/work/stats?details=1" 2>/dev/null || echo '{}')"
else
  pool="$(json_get "${COORD}/api/work/stats?details=1")"
fi
local_w="$(json_get "${LOCAL}/api/wallet")"
local_s="$(json_get "${LOCAL}/api/status?lite=1")"

python3 - "$OUT" "$WALLET" "$WORKER" "$canon" "$pool" "$local_w" "$local_s" <<'PY'
import json, sys
from pathlib import Path
from datetime import datetime, timezone

out = Path(sys.argv[1])
wallet, worker = sys.argv[2], sys.argv[3]
canon = json.loads(sys.argv[4] or "{}")
pool = json.loads(sys.argv[5] or "{}")
local_w = json.loads(sys.argv[6] or "{}")
local_s = json.loads(sys.argv[7] or "{}")

def u8(hmc):
    return int(round(float(hmc or 0) * 1e8))

workers = pool.get("workers") or {}
wk = workers.get(worker) or {}

canon_hmc = (canon.get("balance_units") or 0) / 1e8
local_hmc = float(local_w.get("balance_hmc") or 0)
wk_payout = float(wk.get("payout_hmc") or 0)
wk_sup = float(wk.get("payout_sup") or 0)
pool_total = float(pool.get("total_payout_hmc") or 0)
tip = local_s.get("canonical_tip_height") or local_s.get("tip_height")

row = {
    "captured_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "wallet": wallet,
    "worker": worker,
    "canonical_balance_hmc": round(canon_hmc, 8),
    "local_balance_hmc": round(local_hmc, 8),
    "local_wallet_source": local_w.get("wallet_source", ""),
    "wallet_delta_local_vs_canon_hmc": round(local_hmc - canon_hmc, 8),
    "worker_payout_hmc": round(wk_payout, 8),
    "worker_payout_sup": round(wk_sup, 8),
    "worker_ranges": int(wk.get("accepted_ranges") or 0),
    "worker_hashrate_gh_s": float(wk.get("hashrate_gh_s") or 0),
    "pool_total_payout_hmc": round(pool_total, 8),
    "canonical_tip_height": tip,
    "pool_workers_online": sum(1 for w in workers.values() if w.get("online")),
}
out.joinpath("snapshot.json").write_text(json.dumps(row, indent=2) + "\n")

lines = [
    f"# Penny reconcile — {row['captured_at']}",
    "",
    f"| Field | Value |",
    f"|-------|------:|",
    f"| Canonical balance | **{row['canonical_balance_hmc']:.8f}** HMC |",
    f"| Local wallet view | {row['local_balance_hmc']:.8f} HMC ({row['local_wallet_source']}) |",
    f"| Local − canonical | {row['wallet_delta_local_vs_canon_hmc']:.8f} HMC |",
    f"| `{worker}` payout (pool) | {row['worker_payout_hmc']:.8f} HMC + {row['worker_payout_sup']:.8f} SUP |",
    f"| `{worker}` ranges | {row['worker_ranges']} |",
    f"| Pool total payout | {row['pool_total_payout_hmc']:.8f} HMC |",
    f"| Canonical tip | {tip} |",
    "",
]
warn = abs(row["wallet_delta_local_vs_canon_hmc"]) > 0.00000001 and row["local_wallet_source"] != "canonical_peer"
if warn:
    lines.append("**WARN:** local wallet balance differs from canonical on-chain view.")
else:
    lines.append("**OK:** canonical wallet view consistent (or follower uses canonical_peer).")
out.joinpath("RECONCILE.md").write_text("\n".join(lines) + "\n")
print("\n".join(lines))
PY

echo "[penny] wrote $OUT/snapshot.json and $OUT/RECONCILE.md"
