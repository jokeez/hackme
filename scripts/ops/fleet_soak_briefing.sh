#!/usr/bin/env bash
# Summarize fleet soak metrics (difficulty, payouts, fuzz delta, WAL trend).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SOAK_DIR="${SOAK_DIR:-$ROOT/reports/fleet-soak-10h}"
TSV="${SOAK_DIR}/metrics.tsv"

if [[ ! -f "$TSV" ]]; then
  echo "[soak-briefing] no metrics yet: $TSV"
  exit 1
fi

python3 - "$SOAK_DIR" <<'PY'
import json, sys
from pathlib import Path

soak = Path(sys.argv[1])
tsv = soak / "metrics.tsv"
lines = [ln for ln in tsv.read_text().splitlines()[1:] if ln.strip()]
if not lines:
    print("[soak-briefing] empty metrics")
    raise SystemExit(0)

def parse(line):
    p = line.split("\t")
    return {
        "ts": p[0], "pool_gh": float(p[1]), "target_mod": int(p[2]), "hint": int(p[3]),
        "reward_per_m": float(p[4]), "total_payout": float(p[5]),
        "fleet_live": int(p[6]), "fleet_gh": float(p[7]), "fleet_payout": float(p[8]),
        "fuzz_done": int(p[9]), "wal_mb": float(p[10]), "restarts": int(p[11]),
    }

rows = [parse(ln) for ln in lines]
first, last = rows[0], rows[-1]
print("=== FLEET SOAK BRIEFING ===")
print(f"samples: {len(rows)}  first: {first['ts']}  last: {last['ts']}")
print()
print("--- DIFFICULTY ---")
print(f"  target_mod:  {first['target_mod']:,} → {last['target_mod']:,}")
print(f"  load_hint:   {first['hint']:,} → {last['hint']:,}")
print(f"  pool_gh:     {first['pool_gh']:.1f} → {last['pool_gh']:.1f} GH/s")
mods = [r["target_mod"] for r in rows]
print(f"  M range:     {min(mods):,} … {max(mods):,}")
print()
print("--- PAYOUTS ---")
print(f"  total_payout_hmc: {first['total_payout']:.8f} → {last['total_payout']:.8f}  (Δ {last['total_payout']-first['total_payout']:.8f})")
print(f"  fleet_payout:     {first['fleet_payout']:.8f} → {last['fleet_payout']:.8f}  (Δ {last['fleet_payout']-first['fleet_payout']:.8f})")
print(f"  reward_per_m:     {first['reward_per_m']:.2e} → {last['reward_per_m']:.2e}")
print()
print("--- FUZZ ---")
print(f"  work_done:   {first['fuzz_done']:,} → {last['fuzz_done']:,}  (Δ {last['fuzz_done']-first['fuzz_done']})")
print()
print("--- HUB WAL / RESTARTS ---")
print(f"  WAL MB:      {first['wal_mb']:.1f} → {last['wal_mb']:.1f}")
print(f"  restarts:    {last['restarts']} (10h window)")
print()
print("--- FLEET HEALTH ---")
live = [r["fleet_live"] for r in rows]
print(f"  live workers: min={min(live)} max={max(live)} last={last['fleet_live']}/15")

started = soak / "STARTED.json"
if started.is_file():
    print()
    print(started.read_text().strip())
PY
