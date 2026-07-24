#!/usr/bin/env bash
# Summarize week-ops metrics collected on the hub.
# Usage: bash scripts/ops/week_ops_briefing.sh [/opt/hackme/reports/week-ops-metrics]
set -uo pipefail
JDIR="${1:-${WEEK_OPS_DIR:-/opt/hackme/reports/week-ops-metrics}}"
python3 - "$JDIR" <<'PY'
import json, sys
from pathlib import Path
from statistics import mean

jdir = Path(sys.argv[1])
if not jdir.is_dir():
    print(f"missing dir: {jdir}")
    sys.exit(1)

started = {}
sp = jdir / "STARTED.json"
if sp.is_file():
    started = json.loads(sp.read_text())

snaps = []
sj = jdir / "snapshots.jsonl"
if sj.is_file():
    for ln in sj.read_text(errors="replace").splitlines():
        ln = ln.strip()
        if not ln:
            continue
        try:
            snaps.append(json.loads(ln))
        except Exception:
            pass

print("=== week-ops briefing ===")
print(f"dir: {jdir}")
if started:
    print(f"started: {started.get('started_at')}  planned_end: {started.get('planned_end_at')}")
print(f"samples: {len(snaps)}")
if not snaps:
    print("no snapshots yet")
    sys.exit(0)

first, last = snaps[0], snaps[-1]
print(f"range: {first.get('ts')} → {last.get('ts')}")

def pool_field(s, k, default=0):
    return (s.get("pool") or {}).get(k, default)

ghs = [float(pool_field(s, "gh_s") or 0) for s in snaps]
ons = [int(pool_field(s, "online") or 0) for s in snaps]
unp = [float(pool_field(s, "fleet_unpaid_hmc") or 0) for s in snaps]
runs = [int((s.get("fuzz") or {}).get("runs_done_sum") or 0) for s in snaps]
stuck = [int((s.get("fuzz") or {}).get("stuck_zero_running") or 0) for s in snaps]
settle = [int(s.get("settle_err_tail") or 0) for s in snaps]

print("\n-- pool --")
print(f"gh_s:   min={min(ghs):.1f} avg={mean(ghs):.1f} max={max(ghs):.1f} last={ghs[-1]:.1f}")
print(f"online: min={min(ons)} avg={mean(ons):.1f} max={max(ons)} last={ons[-1]}")
print(f"unpaid_hmc fleet: min={min(unp):.4f} avg={mean(unp):.4f} max={max(unp):.4f} last={unp[-1]:.4f}")
oa = sum(1 for s in snaps if pool_field(s, "orders_active"))
print(f"orders_active samples: {oa}/{len(snaps)}")

print("\n-- fuzz marketplace --")
print(f"runs_done_sum: min={min(runs)} avg={mean(runs):.0f} max={max(runs)} last={runs[-1]}")
print(f"stuck_zero_running: max={max(stuck)} last={stuck[-1]}")
camps = (last.get("fuzz") or {}).get("campaigns") or []
if camps:
    print("latest campaigns:")
    for c in camps[:12]:
        print(f"  {c.get('status','?'):10} {c.get('runs_done')}/{c.get('budget_runs')}  {(c.get('title') or c.get('id') or '')[:60]}")

print("\n-- host (last) --")
h = last.get("host") or {}
print(f"loadavg={h.get('loadavg')}  disk={h.get('disk_blocks_used_avail_usepct')}  mem={h.get('mem_mb_total_used_avail')}")
print(f"du_mb data={h.get('du_data_mb')} logs={h.get('du_logs_mb')} reports={h.get('du_reports_mb')}")
print(f"services: {last.get('services')}")
print(f"libheif: {last.get('libheif')}")

print("\n-- exchange probes (last) --")
for p in last.get("exchange_probes") or []:
    print(f"  {p.get('code')} {p.get('ms')}ms  {p.get('url')}  via={p.get('via','')}")

# per-miner delta unpaid (first→last by worker_id)
print("\n-- per-miner unpaid/settled HMC (first→last sample) --")
def miner_map(s):
    out = {}
    for m in s.get("miners") or []:
        wid = m.get("worker_id")
        if wid:
            out[wid] = m
    return out
m0, m1 = miner_map(first), miner_map(last)
ids = sorted(set(m0) | set(m1), key=lambda i: -float((m1.get(i) or {}).get("unpaid_hmc") or 0))
for wid in ids[:25]:
    a = m0.get(wid) or {}
    b = m1.get(wid) or {}
    u0 = float(a.get("unpaid_hmc") or 0)
    u1 = float(b.get("unpaid_hmc") or 0)
    s0 = float(a.get("settled_hmc") or 0)
    s1 = float(b.get("settled_hmc") or 0)
    g0 = float(a.get("gh_s") or 0)
    g1 = float(b.get("gh_s") or 0)
    print(
        f"  {wid[:36]:36} unpaid {u0:.4f}→{u1:.4f} (Δ{u1-u0:+.4f})  "
        f"settled {s0:.4f}→{s1:.4f} (Δ{s1-s0:+.4f})  gh {g0:.1f}→{g1:.1f}  online={b.get('online')}"
    )

# actions rollup
ai = jdir / "action_items.jsonl"
by = {}
if ai.is_file():
    for ln in ai.read_text(errors="replace").splitlines():
        try:
            a = json.loads(ln)
        except Exception:
            continue
        key = f"{a.get('severity')}:{a.get('area')}:{a.get('msg')}"
        by[key] = by.get(key, 0) + 1
if by:
    print("\n-- action items (count) --")
    for k, n in sorted(by.items(), key=lambda x: -x[1])[:20]:
        print(f"  {n:4}  {k}")
else:
    print("\n-- action items: none --")

print("\n-- files --")
print(f"  {jdir/'latest.json'}")
print(f"  {jdir/'metrics.tsv'}")
print(f"  {jdir/'snapshots.jsonl'}")
print(f"  {jdir/'action_items.jsonl'}")
PY
