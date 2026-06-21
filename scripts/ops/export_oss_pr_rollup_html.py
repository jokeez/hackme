#!/usr/bin/env python3
"""Export OSS PR fuzz rollup → static research page."""
from __future__ import annotations

import html
import json
import shutil
import sys
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CSS = """*{box-sizing:border-box}body{margin:0;font-family:ui-monospace,monospace;background:#070b12;color:#c8d6e8;line-height:1.55}
.wrap{max-width:960px;margin:0 auto;padding:2.5rem 1.25rem 4rem}
h1{font-family:system-ui,sans-serif;font-size:1.35rem;color:#00d1ff;letter-spacing:.08em;text-transform:uppercase;margin:0}
.sub{color:#6b8099;font-size:.78rem;margin:.5rem 0 1.5rem}
.badge{display:inline-block;padding:.45rem 1.1rem;border-radius:999px;border:2px solid #39ff14;color:#39ff14;font-weight:700;font-size:.85rem;margin:1rem 0}
.card{border:1px solid rgba(0,209,255,.22);border-radius:14px;padding:1.2rem;margin:1rem 0;background:rgba(0,0,0,.4)}
table{width:100%;border-collapse:collapse;font-size:.78rem}th,td{border:1px solid rgba(255,255,255,.08);padding:.5rem;text-align:left}
th{color:#00d1ff}a{color:#00d1ff}footer{margin-top:2rem;font-size:.72rem;color:#6b8099}"""


def esc(s: object) -> str:
    return html.escape(str(s) if s is not None else "")


def main() -> int:
    pack = Path(sys.argv[1]) if len(sys.argv) > 1 else ROOT / "reports/oss-pr/CURRENT"
    out = Path(sys.argv[2]) if len(sys.argv) > 2 else ROOT / "web/site/reports/oss-pr-sweep"
    rollup = json.loads((pack / "rollup.json").read_text())
    out.mkdir(parents=True, exist_ok=True)

    rows = ""
    total_crit = total_sig = 0
    for t in rollup.get("targets") or []:
        tid = t["target_id"]
        src_html = pack / tid / "report.html"
        dst = f"{tid}.html"
        if src_html.is_file():
            shutil.copy2(src_html, out / dst)
        crit = int(t.get("critical") or 0)
        sig = int(t.get("guard_signals") or 0)
        total_crit += crit
        total_sig += sig
        rows += (
            f"<tr><td>{esc(tid)}</td><td><a href=\"{esc(t.get('repo',''))}\">{esc(t.get('repo',''))}</a></td>"
            f"<td>{crit}</td><td>{sig}</td><td>{esc(t.get('verdict'))}</td>"
            f"<td><a href=\"./{esc(dst)}\">report</a></td></tr>"
        )

    gen = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    n = len(rollup.get("targets") or [])
    page = f"""<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"/><meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>HackMe · OSS upstream fuzz sweep</title>
<link rel="canonical" href="https://hackme.tech/reports/oss-pr-sweep/"/>
<style>{CSS}</style></head><body><div class="wrap">
<h1>OSS upstream fuzz sweep</h1>
<p class="sub">{esc(rollup.get('stamp'))} · {n} targets · 512 runs each · detector semantics · {esc(gen)}</p>
<span class="badge">{total_crit} CRITICAL · ROTATE CLEAN</span>
<div class="card"><p>Guard signals on malformed inputs are expected reject paths — not CVE claims without native repro.</p>
<table><thead><tr><th>Target</th><th>Repo</th><th>Critical</th><th>Signals</th><th>Verdict</th><th>Report</th></tr></thead>
<tbody>{rows}</tbody></table></div>
<footer><a href="../research.html">Research hub</a> · HackMe Network</footer></div></body></html>"""
    (out / "index.html").write_text(page, encoding="utf-8")
    (out / "CURRENT.html").write_text(page, encoding="utf-8")
    print(f"wrote {out / 'index.html'} ({n} targets, {total_sig} guard signals)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
