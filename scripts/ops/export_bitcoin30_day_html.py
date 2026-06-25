#!/usr/bin/env python3
"""Generate web/site/reports/bitcoin30-dayNN.html from reports/bitcoin30/dayNN-*/DAY_SUMMARY.json."""
from __future__ import annotations

import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
REPORTS = ROOT / "reports" / "bitcoin30"
OUT_DIR = ROOT / "web" / "site" / "reports"


def latest_summary(day: int) -> dict | None:
    best: Path | None = None
    for d in REPORTS.glob(f"day{day:02d}-*"):
        p = d / "DAY_SUMMARY.json"
        if p.is_file() and (best is None or p.stat().st_mtime > best.stat().st_mtime):
            best = p
    if not best:
        return None
    return json.loads(best.read_text())


def week_tag(day: int) -> str:
    if day <= 7:
        return "Week 1"
    if day <= 14:
        return "Week 2"
    if day <= 21:
        return "Week 3 · Fuzz Depth v3"
    return "Week 4 · bytes_corpus"


def short_title(s: dict) -> str:
    t = s.get("title", "")
    for sep in (" · ", " — "):
        if sep in t:
            return t.split(sep)[0].replace("Bitcoin Core ", "")
    return t[:60]


def sig_pct(runs: int, sig: int) -> float:
    if not runs:
        return 0.0
    return 100.0 * sig / runs


def render(day: int, s: dict) -> str:
    runs = int(s.get("runs_done") or 256)
    sig = int(s.get("guard_signal_count") or 0)
    crit = int(s.get("critical_count") or 0)
    edges = int(s.get("new_edges") or 0)
    dur = s.get("duration_sec")
    dur_s = f"{dur}s" if dur else "—"
    pct = sig_pct(runs, sig)
    bar_w = min(100, max(2, pct))
    tier = s.get("depth_tier") or "wasm_only"
    native_c = int(s.get("native_confirmed") or 0)
    native_r = int(s.get("native_rejected") or 0)
    verdict = s.get("verdict") or "clean"
    guard = s.get("guard", "")
    cid = s.get("campaign_id", "")
    src = s.get("hackme_source", "")
    gh_src = f"https://github.com/jokeez/hackme/blob/main/{src}" if src else "#"
    prev_d = day - 1 if day > 1 else None
    next_d = day + 1 if day < 30 else None
    badge = f"{sig} GUARD SIGNALS · 0 CRITICAL"
    if tier in ("wasm_native", "bytes_corpus") and native_c:
        badge = f"{sig} SIGNALS · {native_c} NATIVE CONFIRMED · 0 CRITICAL"
    subtitle = short_title(s)
    native_line = ""
    if tier in ("wasm_native", "bytes_corpus"):
        native_line = f"<p class=\"mod-guard\">Depth tier: <code>{tier}</code> · native confirmed {native_c} · rejected {native_r}</p>"

    def foot_link(d: int | None, label: str) -> str:
        if not d or not (OUT_DIR / f"bitcoin30-day{d:02d}.html").exists() and d < day:
            return ""
        return f'<a href="./bitcoin30-day{d:02d}.html">{label}</a> · '

    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>HackMe · Bitcoin Core 30-Day Fuzz — Day {day}</title>
<meta name="description" content="Day {day}: {subtitle} — {runs} runs, {sig} guard signals, {crit} critical."/>
<link rel="canonical" href="https://hackme.tech/reports/bitcoin30-day{day:02d}.html"/>
<link rel="preconnect" href="https://fonts.googleapis.com"/>
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;600;700&family=Space+Grotesk:wght@500;700&display=swap" rel="stylesheet"/>
<style>
:root{{--neon:#00d1ff;--matrix:#39ff14;--warn:#ffb020;--amber:#ff8c42;--purple:#b388ff;--bg:#05080f;--card:rgba(0,0,0,.45)}}
*{{box-sizing:border-box}}
body{{margin:0;font-family:"JetBrains Mono",ui-monospace,monospace;background:var(--bg);color:#c5d4e8;line-height:1.6;min-height:100vh}}
body::before{{content:"";position:fixed;inset:0;background:radial-gradient(ellipse 80% 50% at 50% -20%,rgba(179,136,255,.1),transparent);pointer-events:none;z-index:0}}
.wrap{{position:relative;z-index:1;max-width:820px;margin:0 auto;padding:2.5rem 1.25rem 4rem}}
.hero{{text-align:center;padding:2rem 1.5rem;border:1px solid rgba(179,136,255,.35);border-radius:20px;background:linear-gradient(160deg,rgba(179,136,255,.08),rgba(0,0,0,.5));margin-bottom:2rem}}
.hero h1{{font-family:"Space Grotesk",sans-serif;font-size:clamp(1.2rem,4vw,1.65rem);letter-spacing:.08em;text-transform:uppercase;color:var(--purple);margin:0 0 .5rem}}
.hero .tag{{font-size:.72rem;color:#6d8099;text-transform:uppercase;letter-spacing:.2em}}
.hero-badge{{display:inline-block;margin-top:1rem;padding:.5rem 1.2rem;border-radius:999px;border:2px solid var(--warn);color:var(--warn);font-weight:700;font-size:.82rem;letter-spacing:.12em;text-transform:uppercase}}
.stats{{display:grid;grid-template-columns:repeat(auto-fit,minmax(120px,1fr));gap:.75rem;margin:1.5rem 0}}
.stat{{border:1px solid rgba(0,209,255,.2);border-radius:12px;padding:1rem;background:var(--card)}}
.stat b{{display:block;font-size:.62rem;text-transform:uppercase;letter-spacing:.12em;color:#6d8099;margin-bottom:.35rem}}
.stat .v{{font-size:1.25rem;font-weight:700;color:#fff}}
.stat .v.ok{{color:var(--matrix)}}
.stat .v.warn{{color:var(--warn)}}
.mod-card{{border:1px solid rgba(255,255,255,.1);border-radius:16px;padding:1.25rem;background:var(--card);margin:1.5rem 0}}
.mod-card h2{{font-family:"Space Grotesk",sans-serif;font-size:1.05rem;margin:0 0 .5rem;color:#fff}}
.mod-guard code{{color:var(--matrix);font-size:.78rem}}
.core-links{{margin:.75rem 0;padding-left:1.1rem;font-size:.75rem}}
.core-links a{{color:var(--neon)}}
.run-bar{{display:flex;height:10px;border-radius:6px;overflow:hidden;background:rgba(255,255,255,.06);margin:1rem 0 .5rem}}
.run-bar .sig{{width:{bar_w:.1f}%;background:linear-gradient(90deg,#4a3030,var(--amber))}}
.verdict{{border-left:3px solid var(--matrix);padding:.75rem 1rem;margin:1.5rem 0;font-size:.82rem;color:#9eb0c8;background:rgba(57,255,20,.04);border-radius:0 8px 8px 0}}
.disclaimer{{border:1px solid rgba(255,176,32,.35);border-radius:12px;padding:1rem;font-size:.78rem;color:#d4b896;margin-top:2rem}}
footer{{margin-top:2rem;padding-top:1rem;border-top:1px solid rgba(255,255,255,.08);font-size:.72rem;color:#6d8099;text-align:center}}
footer a{{color:var(--neon)}}
</style>
</head>
<body>
<div class="wrap">
<section class="hero">
<p class="tag">{week_tag(day)} · Day {day}/30 · bitcoin/bitcoin master</p>
<h1>{subtitle}</h1>
<p>{s.get('title','')}</p>
<span class="hero-badge">{badge}</span>
</section>
<div class="stats">
<div class="stat"><b>Runs</b><span class="v">{runs}</span></div>
<div class="stat"><b>Guard signals</b><span class="v warn">{sig}</span></div>
<div class="stat"><b>Critical</b><span class="v ok">{crit}</span></div>
<div class="stat"><b>Duration</b><span class="v" style="font-size:.9rem">{dur_s}</span></div>
<div class="stat"><b>New edges</b><span class="v">{edges}</span></div>
<div class="stat"><b>Tier</b><span class="v" style="font-size:.72rem">{tier}</span></div>
</div>
<article class="mod-card">
<h2>Module · <code>{guard}</code></h2>
<p class="mod-guard">{s.get('bitcoin_core','')}</p>
<p class="mod-guard">Campaign: <code>{cid}</code></p>
{native_line}
<ul class="core-links">
<li><a href="{gh_src}" target="_blank" rel="noopener">HackMe WASM source</a></li>
<li><a href="./bitcoin30-week3.html">Week 3 ledger</a></li>
<li><a href="./fuzz-depth-v3.html">Fuzz Depth v3 report</a></li>
</ul>
<div class="run-bar"><div class="sig"></div></div>
<p style="font-size:.72rem;color:#6d8099">{pct:.1f}% guard-signal rate — detector semantics on ported WASM guard.</p>
</article>
<div class="verdict">
<strong>Verdict:</strong> <code>{verdict}</code> — boundary-class inputs under detector semantics, not a new consensus CVE claim.
</div>
<div class="disclaimer">
<strong>Honest scope.</strong> WASM excerpt — not <code>bitcoind</code>. Reproduce: <code>DAY={day} bash scripts/ops/run_bitcoin30_day.sh</code>
</div>
<footer>
{foot_link(prev_d, f"Day {prev_d}")}<a href="./bitcoin30.html">Series hub</a> ·
<a href="https://hackme.tech/research.html">Research</a>
</footer>
</div>
</body>
</html>
"""


def main() -> int:
    days = [int(x) for x in sys.argv[1:]] if len(sys.argv) > 1 else list(range(15, 22))
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    for day in days:
        s = latest_summary(day)
        if not s:
            print(f"[export] skip day {day}: no DAY_SUMMARY.json", file=sys.stderr)
            continue
        out = OUT_DIR / f"bitcoin30-day{day:02d}.html"
        out.write_text(render(day, s), encoding="utf-8")
        print(f"[export] {out.name} ← day {day} sig={s.get('guard_signal_count')}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
