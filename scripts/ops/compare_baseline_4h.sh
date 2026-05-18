#!/usr/bin/env bash
# Compare current pool/desktop state vs reports/baseline-4h-* snapshot.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASELINE_DIR="${1:-$(ls -dt "$ROOT_DIR"/reports/baseline-4h-* 2>/dev/null | head -1)}"

if [[ -z "$BASELINE_DIR" || ! -d "$BASELINE_DIR" ]]; then
  echo "No baseline dir. Run baseline capture first." >&2
  exit 1
fi

TOKEN_FILE="${TOKEN_FILE:-$ROOT_DIR/.secrets/hackme_coordinator_admin_token}"
DESK_ENV="${DESK_ENV:-$ROOT_DIR/.env.desktop}"
TOKEN="$(tr -d '\r\n' <"$TOKEN_FILE")"
DESK_ADMIN="$(grep '^HACKME_ADMIN_TOKEN=' "$DESK_ENV" | cut -d= -f2-)"

NOW_DIR="$ROOT_DIR/reports/check-$(date +%Y%m%dT%H%M%S)"
mkdir -p "$NOW_DIR"

curl -fsS -H "X-Hackme-Admin-Token: $TOKEN" \
  "https://hackme.tech/pool/coordinator/api/work/stats?details=1" \
  -o "$NOW_DIR/coordinator_stats.json"
curl -fsS "http://127.0.0.1:8080/api/worker/status" -H "X-Admin-Token: $DESK_ADMIN" \
  -o "$NOW_DIR/desktop_worker.json" 2>/dev/null || true
curl -fsS "http://127.0.0.1:8080/api/wallet" -H "X-Admin-Token: $DESK_ADMIN" \
  -o "$NOW_DIR/desktop_wallet.json" 2>/dev/null || true

python3 - "$BASELINE_DIR" "$NOW_DIR" <<'PY'
import json, sys
from pathlib import Path

base_dir, now_dir = Path(sys.argv[1]), Path(sys.argv[2])
b = json.loads((base_dir / "coordinator_stats.json").read_text())
n = json.loads((now_dir / "coordinator_stats.json").read_text())
bw = json.loads((base_dir / "desktop_worker.json").read_text()) if (base_dir / "desktop_worker.json").exists() else {}
nw = json.loads((now_dir / "desktop_worker.json").read_text()) if (now_dir / "desktop_worker.json").exists() else {}
bwal = json.loads((base_dir / "desktop_wallet.json").read_text()) if (base_dir / "desktop_wallet.json").exists() else {}
nwal = json.loads((now_dir / "desktop_wallet.json").read_text()) if (now_dir / "desktop_wallet.json").exists() else {}

def delta(a, b):
    if a is None or b is None:
        return "n/a"
    return b - a

print(f"Baseline: {base_dir.name}")
print(f"Now:      {now_dir.name}")
print()
print(f"scheduler: {b.get('scheduler_mode')} -> {n.get('scheduler_mode')}")
print(f"orders_active: {b.get('orders_active')} -> {n.get('orders_active')}")
print(f"target_mod: {b.get('target_mod')} -> {n.get('target_mod')} (delta {delta(b.get('target_mod'), n.get('target_mod'))})")
print(f"found_hits: {b.get('found_hits')} -> {n.get('found_hits')}")
print()
print("Workers (attempts / payout_hmc delta):")
all_w = sorted(set(b.get("workers", {})) | set(n.get("workers", {})))
for wid in all_w:
    bo = b.get("workers", {}).get(wid, {})
    no = n.get("workers", {}).get(wid, {})
    da = delta(bo.get("accepted_attempts"), no.get("accepted_attempts"))
    dp = (no.get("payout_hmc") or 0) - (bo.get("payout_hmc") or 0)
    print(f"  {wid}: attempts +{da:,} | payout +{dp:.6f} HMC")
print()
print(f"Desktop worker GH/s: {bw.get('measured_hashrate_gh_s', '?')} -> {nw.get('measured_hashrate_gh_s', '?')} running={nw.get('running')}")
print(f"Wallet balance: {bwal.get('balance_hmc', '?')} -> {nwal.get('balance_hmc', '?')} HMC")
PY

echo ""
echo "Full snapshot: $NOW_DIR"
