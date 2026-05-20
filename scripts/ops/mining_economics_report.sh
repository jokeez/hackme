#!/usr/bin/env bash
# Human-readable economics report from overnight folder or live APIs.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUN_ID="${1:-night_20260520}"
OUT="$ROOT/reports/overnight/$RUN_ID"
WALLET="${WALLET:-HMC-91fe007e4036c602}"

python3 - "$OUT" "$WALLET" <<'PY'
import json, sys
from pathlib import Path

out = Path(sys.argv[1])
wallet = sys.argv[2]
rows = []
for name in ("baseline_snapshot.json", "snapshots.jsonl"):
    p = out / name
    if not p.exists():
        continue
    if name.endswith(".jsonl"):
        for line in p.read_text().splitlines():
            if line.strip():
                rows.append(json.loads(line))
    else:
        rows.append(json.loads(p.read_text()))
if not rows:
    print("no snapshots in", out)
    sys.exit(1)

def g(o, *p, d=0):
    c = o
    for x in p:
        if not isinstance(c, dict):
            return d
        c = c.get(x)
    return d if c is None else c

def pack(r):
    wk = g(r, "local", "work", "workers", d={}) or {}
    ck = g(r, "coordinator", "work", d={}) or wk
    if not isinstance(wk, dict):
        wk = {}
    if not isinstance(ck, dict):
        ck = {}
    return {
        "ts": g(r, "ts", d=g(r, "ts_utc", d="")),
        "bal": float(g(r, "canonical", "wallet", "balance_units", d=0) or 0) / 1e8
        or float(g(r, "local", "wallet", "balance_hmc", d=0) or 0),
        "payout": float(g(r, "coordinator", "work", "total_payout_hmc", d=0)
        or g(r, "local", "work", "total_payout_hmc", d=0) or 0),
        "workers": {
            w: (ck.get(w) or wk.get(w) or {})
            for w in ("worker-kapa-pc", "worker-vps-msk-01", "vps-canary-01")
        },
    }

b, f = pack(rows[0]), pack(rows[-1])
lines = [
    f"# Economics report — {out.name}",
    "",
    f"- **period:** {b['ts']} → {f['ts']}",
    f"- **snapshots:** {len(rows)}",
    f"- **wallet:** `{wallet}`",
    "",
    "## Totals",
    f"- **on-chain balance:** {b['bal']:.8f} → {f['bal']:.8f} HMC (**Δ {f['bal']-b['bal']:+.8f}**)",
    f"- **coordinator accrual:** {b['payout']:.6f} → {f['payout']:.6f} HMC (**Δ {f['payout']-b['payout']:+.6f}**)",
    "",
    "## Per worker (coordinator payout)",
    "| worker | Δ payout HMC | end GH/s | end ranges |",
    "|--------|--------------|----------|------------|",
]
for wid in ("worker-kapa-pc", "worker-vps-msk-01", "vps-canary-01"):
    bp = float((b["workers"].get(wid) or {}).get("payout_hmc") or 0)
    fp = float((f["workers"].get(wid) or {}).get("payout_hmc") or 0)
    gh = float((f["workers"].get(wid) or {}).get("hashrate_gh_s") or 0)
    rg = int((f["workers"].get(wid) or {}).get("accepted_ranges") or 0)
    lines.append(f"| {wid} | {fp-bp:+.6f} | {gh:.4f} | {rg} |")
report = out / "ECONOMICS.md"
report.write_text("\n".join(lines) + "\n")
print(report.read_text())
PY
