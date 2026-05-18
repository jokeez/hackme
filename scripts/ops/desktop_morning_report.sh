#!/usr/bin/env bash
# Human-readable morning report from overnight soak (or live snapshot vs baseline).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${OUT_DIR:-$ROOT/reports/overnight}"
RUN_DIR="${1:-$OUT_DIR/CURRENT}"
SUMMARY="$RUN_DIR/summary.json"
BASELINE="$RUN_DIR/baseline.json"

if [[ ! -f "$SUMMARY" ]]; then
  echo "[morning-report] no summary at $SUMMARY — soak still running or not started." >&2
  if [[ -f "$RUN_DIR/snapshots.jsonl" ]]; then
    echo "[morning-report] snapshots so far: $(wc -l <"$RUN_DIR/snapshots.jsonl") lines in $RUN_DIR/snapshots.jsonl"
  fi
  exit 1
fi

python3 - "$SUMMARY" "$BASELINE" <<'PY'
import json, sys
from pathlib import Path

summary = json.loads(Path(sys.argv[1]).read_text())
b = summary.get("baseline") or {}
f = summary.get("final") or {}
d = summary.get("delta") or {}

def fmt_hmc(x):
    try:
        return f"{float(x):.8f}"
    except Exception:
        return str(x)

print("=" * 60)
print("HackMe overnight report —", summary.get("run_id", "?"))
print("Status:", summary.get("status"), "| snapshots:", summary.get("snapshots", 0))
print("=" * 60)
print()
print("Wallet (canonical on-chain view)")
print("  address:", f.get("wallet_address") or b.get("wallet_address") or "—")
print("  balance start:", fmt_hmc(b.get("wallet_balance_hmc")), "HMC")
print("  balance end:  ", fmt_hmc(f.get("wallet_balance_hmc")), "HMC")
print("  delta:        ", fmt_hmc(d.get("wallet_balance_hmc")), "HMC")
print()
print("Coordinator pool (aggregate)")
print("  total payout start:", fmt_hmc(b.get("coord_total_payout_hmc")), "HMC")
print("  total payout end:  ", fmt_hmc(f.get("coord_total_payout_hmc")), "HMC")
print("  payout delta:      ", fmt_hmc(d.get("coord_total_payout_hmc")), "HMC")
print("  submitted delta:   ", d.get("coord_submitted_items", "—"), "ranges")
print()
for label, key in [("Desktop worker (worker-kapa-pc)", "worker_kapa_pc"), ("VPS worker (worker-active)", "worker_active")]:
    wp = f.get(key) or {}
    bp = b.get(key) or {}
    print(label)
    print("  payout start:", fmt_hmc(bp.get("payout_hmc")), "→ end:", fmt_hmc(wp.get("payout_hmc")))
    print("  ranges delta:", d.get(key + "_ranges", "—"))
    print("  payout delta:", fmt_hmc(d.get(key + "_payout_hmc")), "HMC")
    print()
print("Desktop GPU worker")
print("  running (end):", f.get("desktop_worker_running"))
print("  measured GH/s:", f.get("desktop_measured_gh_s"))
print("  canonical tip delta:", d.get("canonical_tip_height"))
print()
print("Full JSON:", sys.argv[1])
PY
