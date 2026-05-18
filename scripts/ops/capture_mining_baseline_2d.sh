#!/usr/bin/env bash
# Freeze mining state for 1–2 day comparison. Run now; later: compare_baseline_2d.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DIR="$ROOT/reports/baseline-2d-$STAMP"
WALLET="${WALLET:-HMC-91fe007e4036c602}"
TOKEN="$(tr -d '\r\n' <"${ROOT}/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)"
DESK="$(grep '^HACKME_ADMIN_TOKEN=' "${ROOT}/.env.desktop" 2>/dev/null | cut -d= -f2- || true)"

mkdir -p "$DIR"
log() { echo "[baseline-2d] $*" | tee -a "$DIR/capture.log"; }

log "capturing → $DIR"

curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: $TOKEN" \
  "https://hackme.tech/pool/coordinator/api/work/stats?details=1" \
  -o "$DIR/coordinator_stats.json" 2>/dev/null || echo '{}' >"$DIR/coordinator_stats.json"

curl -fsS --max-time 10 "https://hackme.tech/api/address/$(python3 -c "import urllib.parse; print(urllib.parse.quote('$WALLET'))")" \
  -o "$DIR/wallet_canonical.json" 2>/dev/null || true

curl -fsS --max-time 10 "https://hackme.tech/api/global/metrics" \
  -o "$DIR/global_metrics.json" 2>/dev/null || true

curl -fsS --max-time 8 "https://hackme.tech/api/status" -o "$DIR/public_status.json" 2>/dev/null || true

if [[ -n "$DESK" ]]; then
  curl -fsS --max-time 8 "http://127.0.0.1:8080/api/worker/status" \
    -H "X-Hackme-Admin-Token: $DESK" -o "$DIR/desktop_worker.json" 2>/dev/null || true
  curl -fsS --max-time 8 "http://127.0.0.1:8080/api/wallet" \
    -H "X-Hackme-Admin-Token: $DESK" -o "$DIR/desktop_wallet.json" 2>/dev/null || true
fi

# Worker log tail (PC)
LOG="$(ls -t "$ROOT"/logs/workerpoh-worker-kapa-pc-*.log 2>/dev/null | head -1)"
if [[ -n "$LOG" ]]; then
  tail -80 "$LOG" >"$DIR/pc_worker_tail.log"
  grep -c 'submit ok' "$LOG" 2>/dev/null | xargs -I{} echo "submit_ok_total={}" >"$DIR/pc_worker_counts.txt"
  grep -cE 'claim error|submit error' "$LOG" 2>/dev/null | xargs -I{} echo "errors_total={}" >>"$DIR/pc_worker_counts.txt"
fi

pgrep -af workerpoh >"$DIR/processes.txt" 2>/dev/null || true

RELEASE_VER="$(grep -oE 'RELEASE_VER = "[^"]+"' "$ROOT/web/site/assets/app.js" | sed -n 's/.*"\([^"]*\)".*/\1/p')"
{
  echo "# Mining baseline — freeze for 1–2 day check"
  echo ""
  echo "**Captured:** $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "**Wallet:** \`$WALLET\`"
  echo "**Release channel:** \`$RELEASE_VER\`"
  echo ""
  echo "## Compare when you return"
  echo '```bash'
  echo "bash scripts/ops/compare_baseline_2d.sh"
  echo '```'
  echo ""
  echo "## Files"
  echo "- \`coordinator_stats.json\` — pool workers, payouts, target_mod"
  echo "- \`wallet_canonical.json\` — on-chain balance"
  echo "- \`desktop_worker.json\` / \`desktop_wallet.json\` — local node"
} >"$DIR/README.md"

python3 - "$DIR" "$WALLET" <<'PY'
import json, sys
from pathlib import Path
d = Path(sys.argv[1])
wallet = sys.argv[2]
pool = json.loads((d / "coordinator_stats.json").read_text()) if (d / "coordinator_stats.json").exists() else {}
wal = json.loads((d / "wallet_canonical.json").read_text()) if (d / "wallet_canonical.json").exists() else {}
workers = pool.get("workers") or {}
total = sum(w.get("accepted_attempts", 0) for w in workers.values()) or 1
lines = [
    f"scheduler={pool.get('scheduler_mode')} orders={pool.get('orders_active')}",
    f"target_mod={pool.get('target_mod')} found_hits={pool.get('found_hits')}",
    f"lease_sec={pool.get('lease_sec')} claim_per_min={pool.get('claim_per_min')}",
    f"wallet_balance_hmc={(wal.get('balance_units') or 0)/1e8:.8f}",
    "workers:",
]
for wid, w in sorted(workers.items(), key=lambda x: -(x[1].get("accepted_attempts") or 0)):
    att = w.get("accepted_attempts", 0)
    lines.append(f"  {wid}: share={100*att/total:.2f}% payout={w.get('payout_hmc',0):.8f} att={att}")
(d / "SUMMARY.txt").write_text("\n".join(lines) + "\n")
print("\n".join(lines))
PY

ln -sfn "$DIR" "$ROOT/reports/baseline-2d-LATEST"
ln -sfn "$DIR" "$ROOT/reports/baseline-4h-LATEST"

log "done: $DIR"
log "summary:"
cat "$DIR/SUMMARY.txt"
