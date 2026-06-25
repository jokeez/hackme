#!/usr/bin/env python3
"""Generate bitcoin30-weekN.html ledger from DAY_SUMMARY.json files."""
from __future__ import annotations

import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
REPORTS = ROOT / "reports" / "bitcoin30"
OUT = ROOT / "web" / "site" / "reports"


def summaries_for_week(week: int) -> list[dict]:
    lo, hi = {1: (1, 7), 2: (8, 14), 3: (15, 21), 4: (22, 30)}[week]
    out: list[dict] = []
    for day in range(lo, hi + 1):
        best: Path | None = None
        for d in REPORTS.glob(f"day{day:02d}-*"):
            p = d / "DAY_SUMMARY.json"
            if p.is_file() and (best is None or p.stat().st_mtime > best.stat().st_mtime):
                best = p
        if best:
            s = json.loads(best.read_text())
            s["_day"] = day
            out.append(s)
    return out


def render_week3(rows: list[dict]) -> str:
    runs = sum(int(r.get("runs_done") or 256) for r in rows)
    sig = sum(int(r.get("guard_signal_count") or 0) for r in rows)
    crit = sum(int(r.get("critical_count") or 0) for r in rows)
    nat = sum(int(r.get("native_confirmed") or 0) for r in rows)
    cards = ""
    for r in rows:
        d = r["_day"]
        gs = int(r.get("guard_signal_count") or 0)
        nc = int(r.get("native_confirmed") or 0)
        cards += f"""
<article class="mod-card">
<div class="mod-head"><span class="mod-num">{d:02d}</span><div>
<h2><a href="./bitcoin30-day{d:02d}.html" style="color:#fff;text-decoration:none">{r.get('guard','')}</a></h2>
<p class="mod-meta">{gs} guard signals · {nc} native confirmed · tier {r.get('depth_tier','wasm_native')}</p>
</div></div></article>"""
    return f"""<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"/><meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>HackMe · Bitcoin30 Week 3 — wasm_native + native bridge</title>
<link rel="canonical" href="https://hackme.tech/reports/bitcoin30-week3.html"/>
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;600&family=Space+Grotesk:wght@500;700&display=swap" rel="stylesheet"/>
<style>
:root{{--neon:#b388ff;--matrix:#39ff14;--warn:#ffb020;--bg:#05080f;--card:rgba(0,0,0,.45)}}
body{{margin:0;font-family:"JetBrains Mono",monospace;background:var(--bg);color:#c5d4e8;line-height:1.6}}
.wrap{{max-width:920px;margin:0 auto;padding:2rem 1.25rem 4rem}}
.hero{{text-align:center;padding:2rem;border:1px solid rgba(179,136,255,.35);border-radius:20px;margin-bottom:2rem}}
.hero h1{{font-family:"Space Grotesk",sans-serif;color:var(--neon);text-transform:uppercase;letter-spacing:.1em}}
.stats{{display:grid;grid-template-columns:repeat(auto-fit,minmax(120px,1fr));gap:.75rem;margin:1.5rem 0}}
.stat{{border:1px solid rgba(255,255,255,.1);border-radius:12px;padding:1rem;background:var(--card)}}
.stat b{{font-size:.62rem;color:#6d8099;text-transform:uppercase}}
.stat .v{{font-size:1.2rem;font-weight:700;color:#fff}}
.stat .v.ok{{color:var(--matrix)}}.stat .v.warn{{color:var(--warn)}}
.mod-card{{border:1px solid rgba(255,255,255,.1);border-radius:14px;padding:1rem;margin:.75rem 0;background:var(--card)}}
.mod-num{{font-size:1.6rem;color:rgba(179,136,255,.4);font-weight:700}}
.mod-meta{{font-size:.72rem;color:#6d8099}}
footer{{margin-top:2rem;font-size:.72rem;color:#6d8099;text-align:center}}
footer a{{color:var(--neon)}}
</style></head><body><div class="wrap">
<section class="hero">
<p style="font-size:.72rem;color:#6d8099">Fuzz Depth v3 · days 15–21</p>
<h1>Bitcoin30 · Week 3</h1>
<p>wasm_native tier — WASM guard signals confirmed via native bridge before bounty gate.</p>
</section>
<div class="stats">
<div class="stat"><b>Days</b><span class="v">{len(rows)}</span></div>
<div class="stat"><b>Runs</b><span class="v">{runs:,}</span></div>
<div class="stat"><b>Guard signals</b><span class="v warn">{sig}</span></div>
<div class="stat"><b>Native confirmed</b><span class="v ok">{nat}</span></div>
<div class="stat"><b>Critical</b><span class="v ok">{crit}</span></div>
</div>
{cards}
<footer><a href="./bitcoin30.html">Series hub</a> · <a href="./fuzz-depth-v3.html">Fuzz v3</a></footer>
</div></body></html>"""


def main() -> int:
    week = int(sys.argv[1]) if len(sys.argv) > 1 else 3
    rows = summaries_for_week(week)
    if not rows:
        print(f"[week-export] no data for week {week}", file=sys.stderr)
        return 1
    OUT.mkdir(parents=True, exist_ok=True)
    if week == 3:
        html = render_week3(rows)
        path = OUT / "bitcoin30-week3.html"
    elif week == 4:
        html = render_week3(rows).replace("Week 3", "Week 4").replace("wasm_native", "bytes_corpus").replace("days 15–21", "days 22–30").replace("Fuzz Depth v3", "bytes_corpus tier")
        path = OUT / "bitcoin30-week4.html"
    else:
        print(f"[week-export] week {week} template TBD", file=sys.stderr)
        return 0
    path.write_text(html, encoding="utf-8")
    print(f"[week-export] {path.name} ({len(rows)} days)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
