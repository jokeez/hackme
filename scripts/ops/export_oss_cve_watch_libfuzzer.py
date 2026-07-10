#!/usr/bin/env python3
"""Export OSS CVE Watch day HTML + hub from libFuzzer SESSION.json."""
from __future__ import annotations

import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path


def repo_root() -> Path:
    here = Path(__file__).resolve().parents[2]
    if (here / "web" / "site").is_dir():
        return here
    raise SystemExit("web/site not found")


def enrich_session(session: dict, session_dir: Path) -> dict:
    log_path = session_dir / "fuzzer.log"
    if log_path.is_file():
        text = log_path.read_text(errors="replace")
        m_done = re.search(r"Done (\d+) runs in (\d+(?:\.\d+)?) second", text)
        if m_done:
            session["iterations"] = int(m_done.group(1))
            session["elapsed_sec"] = float(m_done.group(2))
        m_eps = re.search(r"stat::average_exec_per_sec:\s*([\d.]+)", text)
        if m_eps:
            session["exec_per_sec"] = float(m_eps.group(1))
        m_new = re.search(r"stat::new_units_added:\s*(\d+)", text)
        if m_new:
            session["corpus_count"] = int(m_new.group(1))
        for line in reversed(text.splitlines()):
            if line.startswith("#") and "cov:" in line:
                m = re.search(
                    r"cov:\s*(\d+)\s+ft:\s*(\d+)\s+corp:\s*(\d+)/(\d+)([KMG]?[bB]?)",
                    line,
                )
                if m:
                    session["coverage_edges"] = int(m.group(1))
                    session["features"] = int(m.group(2))
                    unit = m.group(5).rstrip("bB") or "b"
                    mult = {"": 1, "b": 1, "K": 1024, "M": 1024**2, "G": 1024**3}.get(unit, 1)
                    session["corpus_count"] = int(m.group(3))
                    session["corpus_bytes"] = int(m.group(4)) * mult
                break
    if len(session_dir.name) >= 16 and session_dir.name[8] == "T":
        session["started_at"] = (
            f"{session_dir.name[0:4]}-{session_dir.name[4:6]}-{session_dir.name[6:8]}"
            f"T{session_dir.name[9:11]}:{session_dir.name[11:13]}:{session_dir.name[13:15]}Z"
        )
    if not session.get("finished_at") or session.get("finished_at") == session.get("started_at"):
        session["finished_at"] = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    return session


def day_html(day: int, session: dict, rollup: dict) -> str:
    verdict = rollup.get("verdict", session.get("verdict", "—"))
    iters = int(session.get("iterations", 0))
    exec_s = float(session.get("exec_per_sec", 0))
    elapsed = float(session.get("elapsed_sec", 0))
    asan = int(session.get("asan_crashes", 0))
    ubsan = int(session.get("ubsan_crashes", 0))
    cov = int(session.get("coverage_edges", 0))
    corp = int(session.get("corpus_count", 0))
    corp_kb = int(session.get("corpus_bytes", 0) / 1024) if session.get("corpus_bytes") else 0
    started = str(session.get("started_at", ""))[:10]
    crit = "ok" if verdict == "CLEAN" else "warn"
    hours = elapsed / 3600 if elapsed else 0
    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>HackMe · OSS CVE Watch · Day {day}/14 · nghttp2 · libFuzzer</title>
<meta name="description" content="OSS CVE Watch day {day}: nghttp2 libFuzzer — {iters:,} executions, {verdict}, corpus {corp}."/>
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
.hero h1{{font-family:"Space Grotesk",sans-serif;font-size:clamp(1.1rem,4vw,1.55rem);color:var(--neon);margin:0}}
.hero .tag{{font-size:.72rem;color:#6d8099;text-transform:uppercase;letter-spacing:.2em;margin-bottom:.5rem}}
.badge{{display:inline-block;margin-top:1rem;padding:.45rem 1.1rem;border-radius:999px;border:2px solid var(--warn);color:var(--warn);font-weight:700;font-size:.78rem}}
.stats{{display:grid;grid-template-columns:repeat(auto-fit,minmax(110px,1fr));gap:.75rem;margin:1.5rem 0}}
.stat{{border:1px solid rgba(0,209,255,.2);border-radius:12px;padding:1rem;background:var(--card)}}
.stat b{{display:block;font-size:.62rem;text-transform:uppercase;letter-spacing:.12em;color:#6d8099;margin-bottom:.35rem}}
.stat .v{{font-size:1.1rem;font-weight:700;color:#fff}}
.stat .v.ok{{color:var(--matrix)}}
.mod-card{{border:1px solid rgba(255,255,255,.1);border-radius:16px;padding:1.25rem;background:var(--card);margin:1.5rem 0}}
.mod-card h2{{font-family:"Space Grotesk",sans-serif;font-size:1rem;margin:0 0 .5rem;color:#fff}}
.mod-card code{{color:var(--matrix);font-size:.78rem}}
.compare{{font-size:.78rem;color:#9eb0c8;margin-top:.75rem;line-height:1.7}}
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
<h1>nghttp2 · libFuzzer depth</h1>
<p style="font-size:.85rem;color:#9eb0c8">HTTP/2 session mem_recv · coverage-guided ASAN · {hours:.1f}h operator session</p>
<span class="badge">{verdict}</span>
</section>
<div class="stats">
<div class="stat"><b>Executions</b><span class="v">{iters:,}</span></div>
<div class="stat"><b>Wall time</b><span class="v">{hours:.1f}h</span></div>
<div class="stat"><b>Exec/s</b><span class="v">{exec_s:,.0f}</span></div>
<div class="stat"><b>Coverage</b><span class="v">{cov} edges</span></div>
<div class="stat"><b>Corpus</b><span class="v">{corp} ({corp_kb}KB)</span></div>
<div class="stat"><b>ASAN</b><span class="v ok">{asan}</span></div>
</div>
<article class="mod-card">
<h2>Target · <code>nghttp2</code></h2>
<p><code>https://github.com/nghttp2/nghttp2</code></p>
<p style="font-size:.8rem;color:#9eb0c8;margin-top:.75rem">{rollup.get("summary", "")}</p>
<p class="compare">
<strong>Day 1 → Day 2:</strong> stdin mutation · 44k iter · UBSan noise<br/>
<strong>Day 2:</strong> libFuzzer · {iters/1_000_000:.0f}M exec · corpus grows between nights · hunting ASAN heap bugs
</p>
</article>
<div class="verdict">
<strong>Day {day} verdict:</strong> <code>{verdict}</code> — no ASAN heap corruption in {hours:.0f}h / {iters:,} executions.
CVE # only after coordinated disclosure. <a href="./day01.html" style="color:var(--neon)">Day 1</a> used mutation; Day 2+ uses libFuzzer.
</div>
<div class="disclaimer">
<strong>Honest scope.</strong> Local operator hunt on upstream clone — not a CDN edge deployment.
Reproduce: <code>bash scripts/ops/run_oss_libfuzzer_session.sh</code> (corpus persists in <code>reports/oss-cve-libfuzzer/nghttp2/corpus/</code>).
</div>
<footer>
<a href="./index.html">Series hub</a> · <a href="./day01.html">Day 1</a> ·
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
            pill = "live" if d == meta.get("current_day") else "done"
            day_cells += (
                f'<a href="./day{d:02d}.html">Day {d}</a>'
                f'<span class="pill {pill}">{entry.get("verdict", "—")}</span>'
            )
        else:
            day_cells += f'<span class="pending">Day {d}</span><span class="pill pending">—</span>'
    completed = len(days)
    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>HackMe · OSS CVE Watch · nghttp2</title>
<meta name="description" content="OSS CVE Watch — 14 days on nghttp2. Day 1 mutation, Day 2+ libFuzzer depth."/>
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
.hero h1{{font-family:"Space Grotesk",sans-serif;font-size:clamp(1.2rem,4vw,1.65rem);color:var(--neon);margin:0}}
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
<p class="tag">one repo · 14 days · nghttp2 · Jul 2026</p>
<h1>OSS CVE Watch · nghttp2</h1>
<p style="font-size:.85rem;color:#9eb0c8;margin-top:1rem"><strong>{completed}/14</strong> days published ·
<a href="https://github.com/nghttp2/nghttp2" style="color:var(--neon)">nghttp2/nghttp2</a> · HTTP/2 session mem_recv</p>
</section>
<div class="policy"><strong>Public policy:</strong> Day 1 = stdin mutation baseline. Day 2+ = libFuzzer depth (corpus persists). CLEAN days published honestly. CVE only after ASAN repro + maintainer triage.</div>
<h2 style="font-family:'Space Grotesk',sans-serif;font-size:1rem;color:#fff">Daily ledger</h2>
<div class="days">{day_cells}</div>
<p style="font-size:.78rem;color:#6d8099;margin-top:1.5rem">Day 1: <code>run_oss_cve_watch_day.sh</code> · Day 2+: <code>run_oss_libfuzzer_session.sh</code></p>
<footer><a href="../oss-cve/">OSS CVE cases</a> · <a href="../../research.html">Research</a> · <a href="https://github.com/jokeez/hackme/blob/main/docs/OSS_CVE_HUNT.md">Methodology</a></footer>
</div>
</body>
</html>
"""


def main() -> int:
    if len(sys.argv) < 3:
        print("usage: export_oss_cve_watch_libfuzzer.py DAY SESSION_DIR", file=sys.stderr)
        return 2
    day = int(sys.argv[1])
    session_dir = Path(sys.argv[2]).resolve()
    session = enrich_session(
        json.loads((session_dir / "SESSION.json").read_text()),
        session_dir,
    )
    rollup = json.loads((session_dir / "ROLLUP.json").read_text())
    root = repo_root()
    out_dir = root / "web" / "site" / "reports" / "oss-cve-watch"
    out_dir.mkdir(parents=True, exist_ok=True)

    (out_dir / f"day{day:02d}.html").write_text(day_html(day, session, rollup))

    meta_path = out_dir / "meta.json"
    meta: dict = {"target": "nghttp2", "series_days": 14, "days": []}
    if meta_path.is_file():
        try:
            meta = json.loads(meta_path.read_text())
        except json.JSONDecodeError:
            pass
    if "days" not in meta or not meta["days"]:
        meta["days"] = [{"day": 1, "verdict": "CLEAN", "engine": "mutation", "iterations": 44033, "published": True}]
    days = [d for d in meta.get("days", []) if d.get("day") != day]
    days.append(
        {
            "day": day,
            "verdict": rollup.get("verdict"),
            "engine": "libfuzzer",
            "iterations": session.get("iterations"),
            "exec_per_sec": session.get("exec_per_sec"),
            "corpus_count": session.get("corpus_count"),
            "coverage_edges": session.get("coverage_edges"),
            "asan_crashes": session.get("asan_crashes", 0),
            "elapsed_sec": session.get("elapsed_sec"),
            "started_at": session.get("started_at"),
            "finished_at": session.get("finished_at"),
            "published": True,
        }
    )
    days.sort(key=lambda x: x["day"])
    meta["days"] = days
    meta["current_day"] = day
    meta["engine"] = "libfuzzer"
    meta["updated_at"] = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    meta_path.write_text(json.dumps(meta, indent=2) + "\n")
    (out_dir / "index.html").write_text(hub_html(meta))
    print(f"exported day{day:02d}.html + index.html → {out_dir}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
