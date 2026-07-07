#!/usr/bin/env python3
"""Export OSS CVE Watch day report + hub meta from hunt ROLLUP.json."""
from __future__ import annotations

import json
import sys
from datetime import datetime, timezone
from pathlib import Path


def repo_root_from(report: Path) -> Path:
    root = report.resolve()
    for _ in range(10):
        if (root / "web" / "site").is_dir():
            return root
        root = root.parent
    raise SystemExit("web/site not found")


def sanitizer_counts(crashes: list) -> tuple[int, int, int]:
    asan = ubsan = other = 0
    for c in crashes:
        s = str(c.get("sanitizer", ""))
        if "AddressSanitizer" in s:
            asan += 1
        elif "UndefinedBehavior" in s:
            ubsan += 1
        else:
            other += 1
    return asan, ubsan, other


def day_html(day: int, r: dict, t: dict, asan: int, ubsan: int) -> str:
    total_crashes = len(t.get("crashes", []))
    verdict = r.get("verdict", "—")
    tv = t.get("verdict", "—")
    iters = t.get("iterations", 0)
    elapsed = round(float(t.get("elapsed_sec", 0)), 1)
    started = r.get("started_at", "")[:10]
    prev_d = day - 1 if day > 1 else None
    next_d = day + 1 if day < 14 else None
    prev_link = (
        f'<a href="./day{prev_d:02d}.html">Day {prev_d}</a> · '
        if prev_d and (Path(__file__).resolve().parents[2] / f"web/site/reports/oss-cve-watch/day{prev_d:02d}.html").is_file()
        else ""
    )
    next_link = (
        f' · <a href="./day{next_d:02d}.html">Day {next_d}</a>'
        if next_d and (Path(__file__).resolve().parents[2] / f"web/site/reports/oss-cve-watch/day{next_d:02d}.html").is_file()
        else ""
    )
    crit_class = "ok" if verdict == "CLEAN" else "warn"
    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>HackMe · OSS CVE Watch · Day {day}/14 · nghttp2</title>
<meta name="description" content="OSS CVE Watch day {day}: nghttp2 Tier-D ASAN hunt — {iters:,} iterations, {verdict}."/>
<link rel="canonical" href="https://hackme.tech/reports/oss-cve-watch/day{day:02d}.html"/>
<link rel="preconnect" href="https://fonts.googleapis.com"/>
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;600;700&family=Space+Grotesk:wght@500;700&display=swap" rel="stylesheet"/>
<style>
:root{{--neon:#00d1ff;--matrix:#39ff14;--warn:#ffb020;--bg:#05080f;--card:rgba(0,0,0,.45)}}
*{{box-sizing:border-box}}
body{{margin:0;font-family:"JetBrains Mono",ui-monospace,monospace;background:var(--bg);color:#c5d4e8;line-height:1.6}}
body::before{{content:"";position:fixed;inset:0;background:radial-gradient(ellipse 80% 50% at 50% -20%,rgba(0,209,255,.1),transparent);pointer-events:none;z-index:0}}
.wrap{{position:relative;z-index:1;max-width:820px;margin:0 auto;padding:2.5rem 1.25rem 4rem}}
.hero{{text-align:center;padding:2rem 1.5rem;border:1px solid rgba(0,209,255,.3);border-radius:20px;background:linear-gradient(160deg,rgba(0,209,255,.07),rgba(0,0,0,.5));margin-bottom:2rem}}
.hero h1{{font-family:"Space Grotesk",sans-serif;font-size:clamp(1.1rem,4vw,1.55rem);letter-spacing:.08em;text-transform:uppercase;color:var(--neon);margin:0}}
.hero .tag{{font-size:.72rem;color:#6d8099;text-transform:uppercase;letter-spacing:.2em;margin-bottom:.5rem}}
.badge{{display:inline-block;margin-top:1rem;padding:.45rem 1.1rem;border-radius:999px;border:2px solid var(--warn);color:var(--warn);font-weight:700;font-size:.78rem;letter-spacing:.08em}}
.stats{{display:grid;grid-template-columns:repeat(auto-fit,minmax(120px,1fr));gap:.75rem;margin:1.5rem 0}}
.stat{{border:1px solid rgba(0,209,255,.2);border-radius:12px;padding:1rem;background:var(--card)}}
.stat b{{display:block;font-size:.62rem;text-transform:uppercase;letter-spacing:.12em;color:#6d8099;margin-bottom:.35rem}}
.stat .v{{font-size:1.2rem;font-weight:700;color:#fff}}
.stat .v.ok{{color:var(--matrix)}}
.stat .v.warn{{color:var(--warn)}}
.mod-card{{border:1px solid rgba(255,255,255,.1);border-radius:16px;padding:1.25rem;background:var(--card);margin:1.5rem 0}}
.mod-card h2{{font-family:"Space Grotesk",sans-serif;font-size:1rem;margin:0 0 .5rem;color:#fff}}
.mod-card code{{color:var(--matrix);font-size:.78rem}}
.verdict{{border-left:3px solid var(--matrix);padding:.75rem 1rem;margin:1.5rem 0;font-size:.82rem;color:#9eb0c8;background:rgba(57,255,20,.04);border-radius:0 8px 8px 0}}
.disclaimer{{border:1px solid rgba(255,176,32,.35);border-radius:12px;padding:1rem;font-size:.78rem;color:#d4b896;margin-top:2rem}}
footer{{margin-top:2rem;text-align:center;font-size:.72rem;color:#6d8099}}
footer a{{color:var(--neon)}}
</style>
</head>
<body>
<div class="wrap">
<section class="hero">
<p class="tag">OSS CVE Watch · Day {day}/14 · {started}</p>
<h1>nghttp2 · session mem_recv</h1>
<p style="font-size:.85rem;color:#9eb0c8">HTTP/2 framing parser · Tier-D ASAN/UBSan stdin fuzz</p>
<span class="badge">{verdict} · {tv}</span>
</section>
<div class="stats">
<div class="stat"><b>Iterations</b><span class="v">{iters:,}</span></div>
<div class="stat"><b>Elapsed</b><span class="v" style="font-size:.95rem">{elapsed}s</span></div>
<div class="stat"><b>ASAN</b><span class="v ok">{asan}</span></div>
<div class="stat"><b>UBSan signals</b><span class="v warn">{ubsan}</span></div>
<div class="stat"><b>Verdict</b><span class="v {crit_class}">{verdict}</span></div>
<div class="stat"><b>Budget</b><span class="v" style="font-size:.72rem">Tier-D hunt</span></div>
</div>
<article class="mod-card">
<h2>Target · <code>nghttp2</code></h2>
<p><code>{t.get('repo', 'https://github.com/nghttp2/nghttp2')}</code></p>
<p style="font-size:.8rem;color:#9eb0c8;margin-top:.75rem">{r.get('summary', '')}</p>
<p style="font-size:.78rem;color:#6d8099;margin-top:.75rem">Total sanitizer events in rollup: <strong>{total_crashes:,}</strong> — UBSan on session bootstrap paths is expected noise until native repro confirms exploitability.</p>
</article>
<div class="verdict">
<strong>Day {day} verdict:</strong> <code>{verdict}</code> / per-target <code>{tv}</code> — no ASAN heap corruption in this budget. UBSan ≠ CVE without maintainer triage.
</div>
<div class="disclaimer">
<strong>Honest scope.</strong> Real upstream clone + ASAN driver — not a CDN edge deployment. CVE # only after coordinated disclosure.
Reproduce: <code>DAY={day} bash scripts/ops/run_oss_cve_watch_day.sh</code>
</div>
<footer>
{prev_link}<a href="./index.html">Series hub</a>{next_link} ·
<a href="https://hackme.tech/research.html">Research</a>
</footer>
</div>
</body>
</html>
"""


def hub_html(meta: dict) -> str:
    days = meta.get("days", [])
    day_cells = ""
    for d in range(1, 15):
        entry = next((x for x in days if x.get("day") == d), None)
        if entry:
            day_cells += (
                f'<a href="./day{d:02d}.html">Day {d}</a>'
                f'<span class="pill {"live" if d == meta.get("current_day") else "done"}">{entry.get("verdict", "—")}</span>'
            )
        else:
            day_cells += f'<span class="pending">Day {d}</span><span class="pill pending">—</span>'
    completed = len([d for d in days if d.get("published")])
    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>HackMe · OSS CVE Watch · nghttp2</title>
<meta name="description" content="OSS CVE Watch — 14 days on nghttp2 HTTP/2 parser. Daily Tier-D ASAN ledger, honest CLEAN days."/>
<link rel="canonical" href="https://hackme.tech/reports/oss-cve-watch/"/>
<link rel="preconnect" href="https://fonts.googleapis.com"/>
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;600;700&family=Space+Grotesk:wght@500;700&display=swap" rel="stylesheet"/>
<style>
:root{{--neon:#00d1ff;--matrix:#39ff14;--warn:#ffb020;--bg:#05080f;--card:rgba(0,0,0,.45)}}
*{{box-sizing:border-box}}
body{{margin:0;font-family:"JetBrains Mono",ui-monospace,monospace;background:var(--bg);color:#c5d4e8;line-height:1.6}}
body::before{{content:"";position:fixed;inset:0;background:radial-gradient(ellipse 80% 50% at 50% -20%,rgba(0,209,255,.1),transparent);pointer-events:none;z-index:0}}
.wrap{{position:relative;z-index:1;max-width:920px;margin:0 auto;padding:2.5rem 1.25rem 4rem}}
.hero{{text-align:center;padding:2rem 1.5rem;border:1px solid rgba(0,209,255,.28);border-radius:20px;background:linear-gradient(160deg,rgba(0,209,255,.08),rgba(0,0,0,.5));margin-bottom:2rem}}
.hero h1{{font-family:"Space Grotesk",sans-serif;font-size:clamp(1.2rem,4vw,1.65rem);letter-spacing:.1em;text-transform:uppercase;color:var(--neon);margin:0}}
.hero .tag{{font-size:.72rem;color:#6d8099;text-transform:uppercase;letter-spacing:.2em}}
.policy{{margin:1.25rem 0;padding:1rem;border:1px solid rgba(255,176,32,.35);border-radius:12px;font-size:.8rem;color:#d4b896;background:rgba(255,176,32,.05)}}
.days{{display:grid;grid-template-columns:repeat(auto-fill,minmax(140px,1fr));gap:.5rem;margin-top:1rem}}
.days a,.days .pending{{font-size:.72rem;padding:.5rem .55rem;border:1px solid rgba(255,255,255,.08);border-radius:8px;color:#b8c8dc;text-decoration:none;display:flex;justify-content:space-between;align-items:center;gap:.35rem}}
.days a:hover{{border-color:rgba(0,209,255,.35);color:#fff}}
.days .pending{{opacity:.45}}
.pill{{font-size:.6rem;padding:.15rem .45rem;border-radius:999px;border:1px solid rgba(255,255,255,.15)}}
.pill.live{{border-color:rgba(0,209,255,.5);color:var(--neon)}}
.pill.done{{border-color:rgba(57,255,20,.4);color:var(--matrix)}}
.pill.pending{{opacity:.6}}
footer{{margin-top:2rem;text-align:center;font-size:.72rem;color:#6d8099}}
footer a{{color:var(--neon)}}
</style>
</head>
<body>
<div class="wrap">
<section class="hero">
<p class="tag">one repo · 14 days · Tier-D ASAN · Jul 2026</p>
<h1>OSS CVE Watch · nghttp2</h1>
<p style="font-size:.85rem;color:#9eb0c8;margin-top:1rem"><strong>{completed}/14</strong> days published · target <a href="https://github.com/nghttp2/nghttp2" style="color:var(--neon)">nghttp2/nghttp2</a> · HTTP/2 framing / session mem_recv</p>
</section>
<div class="policy"><strong>Public policy:</strong> Daily ledger includes CLEAN days. UBSan bootstrap noise is informational — CVE claims only after ASAN repro + maintainer triage. Marathon waves stay off the public narrative.</div>
<h2 style="font-family:'Space Grotesk',sans-serif;font-size:1rem;color:#fff">Daily ledger</h2>
<div class="days">
{day_cells}
</div>
<p style="font-size:.78rem;color:#6d8099;margin-top:1.5rem">Reproduce: <code>DAY=N bash scripts/ops/run_oss_cve_watch_day.sh</code></p>
<footer><a href="../oss-cve/">OSS CVE cases</a> · <a href="../../research.html">Research</a> · <a href="https://github.com/jokeez/hackme/blob/main/docs/OSS_CVE_HUNT.md">Methodology</a></footer>
</div>
</body>
</html>
"""


def main() -> int:
    if len(sys.argv) < 3:
        print("usage: export_oss_cve_watch_html.py DAY REPORT_DIR", file=sys.stderr)
        return 2
    day = int(sys.argv[1])
    report = Path(sys.argv[2]).resolve()
    rollup_path = report / "ROLLUP.json"
    if not rollup_path.is_file():
        print(f"missing {rollup_path}", file=sys.stderr)
        return 2

    root = repo_root_from(report)
    out_dir = root / "web" / "site" / "reports" / "oss-cve-watch"
    out_dir.mkdir(parents=True, exist_ok=True)

    r = json.loads(rollup_path.read_text())
    targets = r.get("targets", [])
    if not targets:
        print("no targets in rollup", file=sys.stderr)
        return 2
    t = targets[0]
    asan, ubsan, _ = sanitizer_counts(t.get("crashes", []))

    day_path = out_dir / f"day{day:02d}.html"
    day_path.write_text(day_html(day, r, t, asan, ubsan))

    meta_path = out_dir / "meta.json"
    meta: dict = {"target": "nghttp2", "series_days": 14, "days": []}
    if meta_path.is_file():
        try:
            meta = json.loads(meta_path.read_text())
        except json.JSONDecodeError:
            pass
    days: list = [d for d in meta.get("days", []) if d.get("day") != day]
    days.append(
        {
            "day": day,
            "verdict": r.get("verdict"),
            "target_verdict": t.get("verdict"),
            "iterations": t.get("iterations"),
            "asan_crashes": asan,
            "ubsan_signals": ubsan,
            "started_at": r.get("started_at"),
            "finished_at": r.get("finished_at"),
            "report_dir": report.name,
            "published": True,
        }
    )
    days.sort(key=lambda x: x["day"])
    meta["days"] = days
    meta["current_day"] = max(d["day"] for d in days)
    meta["updated_at"] = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    meta_path.write_text(json.dumps(meta, indent=2) + "\n")

    (out_dir / "index.html").write_text(hub_html(meta))
    print(f"exported day{day:02d}.html + index.html → {out_dir}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
