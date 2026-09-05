#!/usr/bin/env python3
"""Export Hunt 12-day watch series rollup (HTML + Markdown + JSON).

  SERIES=2026sep python3 scripts/ops/export_hunt_watch_rollup.py
  SERIES=2026sep OUT=reports/hunt-watch/2026sep/ROLLUP.html \\
    python3 scripts/ops/export_hunt_watch_rollup.py

Scans reports/hunt-watch/<series>/day*/hunt-*.json (skips hunt-report-*).
Includes a fixed known-issue appendix for the obscure pilot disclosures.
"""
from __future__ import annotations

import json
import os
import re
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SERIES = os.environ.get("SERIES", "2026sep")
BASE = Path(os.environ.get("WATCH_BASE", ROOT / "reports" / "hunt-watch" / SERIES))
OUT_HTML = Path(os.environ.get("OUT", BASE / "ROLLUP.html"))
OUT_MD = Path(os.environ.get("OUT_MD", BASE / "ROLLUP.md"))
OUT_JSON = Path(os.environ.get("OUT_JSON", BASE / "ROLLUP.json"))

# Honest disclosure appendix (obscure pilot — not part of day01–12 rotation).
KNOWN_ISSUES = [
    {
        "target": "centijson",
        "verdict": "INFORMATIONAL",
        "sanitizer": "ubsan/null-deref (memcpy)",
        "status": "fixed_upstream",
        "detail": "Empty-string input \"\" triggered UBSan at value.c:438 on an older shallow tip. "
        "Maintainer pointed to 7d4ab62 (guard memcpy when len==0). Retested on current master: CLEAN.",
        "links": [
            "https://github.com/mity/centijson/issues/16",
            "https://github.com/mity/centijson/commit/7d4ab62",
        ],
    },
    {
        "target": "jsonparser",
        "verdict": "INFORMATIONAL",
        "sanitizer": "ubsan/null-pointer-offset",
        "status": "known_open_duplicate",
        "detail": "3-byte input {\"\" hit json.c:437. Upstream closed our #186 as duplicate of open #166 (2022).",
        "links": [
            "https://github.com/json-parser/json-parser/issues/186",
            "https://github.com/json-parser/json-parser/issues/166",
        ],
    },
]


def day_num(name: str) -> int:
    m = re.match(r"day(\d+)", name)
    return int(m.group(1)) if m else 0


def load_rows() -> list[dict]:
    rows: list[dict] = []
    if not BASE.is_dir():
        return rows
    for day_dir in sorted(BASE.glob("day*"), key=lambda p: (day_num(p.name), p.name)):
        if not day_dir.is_dir():
            continue
        for path in sorted(day_dir.glob("hunt-*.json")):
            if path.name.startswith("hunt-report-"):
                continue
            try:
                d = json.loads(path.read_text())
            except Exception:
                continue
            d["_day_dir"] = day_dir.name
            d["_day"] = day_num(day_dir.name)
            d["_path"] = str(path.relative_to(ROOT)) if path.is_relative_to(ROOT) else str(path)
            rows.append(d)
    return rows


def summarize(rows: list[dict]) -> dict:
    by_verdict: dict[str, int] = {}
    total_iter = 0
    total_crashes = 0
    days = sorted({r["_day"] for r in rows})
    for r in rows:
        v = str(r.get("verdict") or "UNKNOWN")
        by_verdict[v] = by_verdict.get(v, 0) + 1
        total_iter += int(r.get("iterations") or 0)
        total_crashes += int(r.get("crashes") or 0)
    return {
        "series": SERIES,
        "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "days_present": days,
        "targets_completed": len(rows),
        "total_iterations": total_iter,
        "total_crashes": total_crashes,
        "by_verdict": by_verdict,
        "engine": "hunt_standard",
        "branch": "feature/hunt-mvp",
        "known_issues": KNOWN_ISSUES,
        "rows": rows,
    }


def md(doc: dict) -> str:
    lines = [
        f"# Hunt watch rollup — `{doc['series']}`",
        "",
        f"- generated: **{doc['generated_at']}**",
        f"- branch: [`feature/hunt-mvp`](https://github.com/jokeez/hackme/tree/feature/hunt-mvp)",
        f"- engine: **{doc['engine']}** (ASAN+UBSan, not libFuzzer)",
        f"- targets completed: **{doc['targets_completed']}**",
        f"- total iterations: **{doc['total_iterations']:,}**",
        f"- crashes (series): **{doc['total_crashes']}**",
        f"- verdicts: `{json.dumps(doc['by_verdict'])}`",
        "",
        "## Results",
        "",
        "| Day | Target | Verdict | Iterations | exec/s | Crashes |",
        "|-----|--------|---------|------------|--------|---------|",
    ]
    for r in doc["rows"]:
        eps = float(r.get("exec_per_sec") or 0)
        lines.append(
            f"| {r['_day']} | {r.get('target','?')} | {r.get('verdict','?')} | "
            f"{int(r.get('iterations') or 0):,} | {eps:.1f} | {r.get('crashes',0)} |"
        )
    lines += [
        "",
        "## Known issues (obscure pilot — disclosure)",
        "",
        "These are **not** part of the day-rotation CLEAN ledger. Honest status for public posts:",
        "",
    ]
    for ki in doc["known_issues"]:
        links = ", ".join(f"[link]({u})" for u in ki["links"])
        lines += [
            f"### {ki['target']} — {ki['status']}",
            "",
            f"- verdict at find: **{ki['verdict']}** · `{ki['sanitizer']}`",
            f"- {ki['detail']}",
            f"- {links}",
            "",
        ]
    lines += [
        "## Interpretation",
        "",
        "- **CLEAN** on mature parsers is a normal Hunt Standard outcome.",
        "- Hunt value = verified sanitizer audit + report, not exec/s vs libFuzzer.",
        "- Re-run export anytime: `python3 scripts/ops/export_hunt_watch_rollup.py`",
        "",
    ]
    return "\n".join(lines)


def html(doc: dict) -> str:
    rows_html = []
    for r in doc["rows"]:
        v = str(r.get("verdict") or "?")
        cls = "ok" if v == "CLEAN" else "warn"
        eps = float(r.get("exec_per_sec") or 0)
        rows_html.append(
            f"<tr><td>{r['_day']}</td><td><code>{r.get('target','?')}</code></td>"
            f"<td class=\"{cls}\">{v}</td><td>{int(r.get('iterations') or 0):,}</td>"
            f"<td>{eps:.1f}</td><td>{r.get('crashes',0)}</td></tr>"
        )
    issues_html = []
    for ki in doc["known_issues"]:
        links = " · ".join(f'<a href="{u}" rel="noopener">{u.rsplit("/",1)[-1]}</a>' for u in ki["links"])
        issues_html.append(
            f"<div class=\"card\"><h3>{ki['target']} <span class=\"tag\">{ki['status']}</span></h3>"
            f"<p><code>{ki['sanitizer']}</code> · find verdict <strong>{ki['verdict']}</strong></p>"
            f"<p>{ki['detail']}</p><p class=\"links\">{links}</p></div>"
        )
    verdict_bits = " · ".join(f"{k}: {v}" for k, v in sorted(doc["by_verdict"].items()))
    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>HackMe Hunt · Watch rollup · {doc['series']}</title>
<meta name="description" content="Hunt Standard 12-day watch rollup: {doc['targets_completed']} targets, {doc['total_iterations']:,} iterations."/>
<link rel="preconnect" href="https://fonts.googleapis.com"/>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;600&family=Syne:wght@600;700&display=swap" rel="stylesheet"/>
<style>
:root{{--bg:#0b0f14;--ink:#e8eef5;--muted:#8b9bb0;--line:#1e2a38;--ok:#3dffa8;--warn:#ffb020;--accent:#7c9cff}}
*{{box-sizing:border-box}}
body{{margin:0;font-family:"IBM Plex Mono",ui-monospace,monospace;background:
  radial-gradient(1200px 600px at 10% -10%,rgba(124,156,255,.12),transparent),
  radial-gradient(900px 500px at 90% 0%,rgba(61,255,168,.06),transparent),
  var(--bg);color:var(--ink);line-height:1.55}}
.wrap{{max-width:920px;margin:0 auto;padding:2.5rem 1.25rem 4rem}}
h1{{font-family:Syne,sans-serif;font-size:clamp(1.4rem,4vw,2rem);margin:0 0 .35rem;letter-spacing:-.02em}}
.sub{{color:var(--muted);font-size:.85rem;margin-bottom:1.75rem}}
.stats{{display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:.75rem;margin:1.5rem 0}}
.stat{{border:1px solid var(--line);border-radius:14px;padding:1rem;background:rgba(255,255,255,.02)}}
.stat b{{display:block;font-size:.65rem;text-transform:uppercase;letter-spacing:.12em;color:var(--muted);margin-bottom:.35rem}}
.stat .v{{font-size:1.25rem;font-weight:600}}
table{{width:100%;border-collapse:collapse;font-size:.82rem;margin:1rem 0 2rem}}
th,td{{border-bottom:1px solid var(--line);padding:.55rem .4rem;text-align:left}}
th{{color:var(--muted);font-weight:600;font-size:.68rem;text-transform:uppercase;letter-spacing:.08em}}
td.ok{{color:var(--ok)}} td.warn{{color:var(--warn)}}
.card{{border:1px solid var(--line);border-radius:14px;padding:1.1rem 1.2rem;margin:0 0 .9rem;background:rgba(255,255,255,.02)}}
.card h3{{margin:0 0 .5rem;font-family:Syne,sans-serif;font-size:1rem}}
.tag{{font-size:.65rem;border:1px solid var(--accent);color:var(--accent);padding:.15rem .45rem;border-radius:999px;margin-left:.35rem;vertical-align:middle}}
.links a{{color:var(--accent)}}
footer{{margin-top:2.5rem;color:var(--muted);font-size:.75rem}}
a{{color:var(--accent)}}
</style>
</head>
<body>
<div class="wrap">
  <h1>Hunt watch · {doc['series']}</h1>
  <p class="sub">HackMe Hunt Standard · ASAN+UBSan · branch <a href="https://github.com/jokeez/hackme/tree/feature/hunt-mvp">feature/hunt-mvp</a> · generated {doc['generated_at']}</p>
  <div class="stats">
    <div class="stat"><b>Targets</b><div class="v">{doc['targets_completed']}</div></div>
    <div class="stat"><b>Iterations</b><div class="v">{doc['total_iterations']:,}</div></div>
    <div class="stat"><b>Crashes</b><div class="v">{doc['total_crashes']}</div></div>
    <div class="stat"><b>Verdicts</b><div class="v" style="font-size:.9rem">{verdict_bits or '—'}</div></div>
  </div>
  <h2 style="font-family:Syne,sans-serif;font-size:1.1rem">Day rotation results</h2>
  <table>
    <thead><tr><th>Day</th><th>Target</th><th>Verdict</th><th>Iter</th><th>exec/s</th><th>Crashes</th></tr></thead>
    <tbody>
      {''.join(rows_html)}
    </tbody>
  </table>
  <h2 style="font-family:Syne,sans-serif;font-size:1.1rem">Known issues (obscure pilot)</h2>
  <p class="sub">Disclosure appendix — separate from the CLEAN day ledger above.</p>
  {''.join(issues_html)}
  <footer>
    Not a CVE lottery. Hunt = verified sanitizer audit + report.
    Re-export: <code>python3 scripts/ops/export_hunt_watch_rollup.py</code>
  </footer>
</div>
</body>
</html>
"""


def main() -> int:
    rows = load_rows()
    doc = summarize(rows)
    # strip private keys for JSON export
    export = {k: v for k, v in doc.items() if k != "rows"}
    export["rows"] = [
        {
            "day": r["_day"],
            "day_dir": r["_day_dir"],
            "target": r.get("target"),
            "verdict": r.get("verdict"),
            "iterations": r.get("iterations"),
            "exec_per_sec": r.get("exec_per_sec"),
            "crashes": r.get("crashes"),
            "elapsed_sec": r.get("elapsed_sec"),
        }
        for r in rows
    ]
    OUT_HTML.parent.mkdir(parents=True, exist_ok=True)
    OUT_HTML.write_text(html(doc))
    OUT_MD.write_text(md(doc))
    OUT_JSON.write_text(json.dumps(export, indent=2) + "\n")
    print(f"[hunt-rollup] targets={doc['targets_completed']} iter={doc['total_iterations']}")
    print(f"[hunt-rollup] HTML → {OUT_HTML}")
    print(f"[hunt-rollup] MD   → {OUT_MD}")
    print(f"[hunt-rollup] JSON → {OUT_JSON}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
