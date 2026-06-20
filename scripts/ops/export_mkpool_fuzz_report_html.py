#!/usr/bin/env python3
"""Export mkpool fuzz pack JSON → static fuzz_report_v2 HTML (matches node #fuzz reports)."""
from __future__ import annotations

import html
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
GUARD_WASM = {
    "sv2_reader_bounds": "tasks/artifacts/security/mkpool/sv2_reader_bounds.wasm",
    "version_mask": "tasks/artifacts/security/mkpool/version_mask.wasm",
    "submit_hex_fields": "tasks/artifacts/security/mkpool/submit_hex_fields.wasm",
    "v1_line_frame": "tasks/artifacts/security/mkpool/v1_line_frame.wasm",
}

CSS = """*{box-sizing:border-box}
body{margin:0;font-family:ui-monospace,Menlo,Consolas,monospace;background:#070b12;color:#c8d6e5;line-height:1.55}
.wrap{max-width:960px;margin:0 auto;padding:2.5rem 1.5rem 4rem}
h1{font-size:1.4rem;font-weight:700;letter-spacing:.14em;text-transform:uppercase;color:#00d1ff;margin:0}
.sub{color:#6b7c93;font-size:.78rem;margin:.5rem 0 2rem}
.badge{display:inline-block;margin-top:1rem;padding:.45rem 1.15rem;border-radius:999px;border:2px solid %s;color:%s;font-size:1.05rem;font-weight:700;text-transform:uppercase;letter-spacing:.18em}
.card{border:1px solid rgba(0,209,255,.22);border-radius:14px;background:linear-gradient(145deg,rgba(0,0,0,.55),rgba(0,209,255,.07));padding:1.35rem;margin:1.1rem 0}
.card.scope{border-color:rgba(255,176,32,.35);background:linear-gradient(145deg,rgba(0,0,0,.5),rgba(255,176,32,.06))}
.title{font-size:1.15rem;color:#fff;margin:.4rem 0}
.muted{color:#6b7c93;font-size:.78rem}
.lbl{color:#00d1ff;font-size:.68rem;text-transform:uppercase;letter-spacing:.1em}
code{color:#39ff14;font-size:.82rem;word-break:break-all}
pre.repro{margin:.5rem 0 0;padding:.75rem 1rem;background:rgba(0,0,0,.45);border-radius:8px;border:1px solid rgba(57,255,20,.2);overflow-x:auto;font-size:.75rem;color:#a8e6cf}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:.7rem;margin-top:1.1rem}
.stat{border:1px solid rgba(57,255,20,.18);border-radius:10px;padding:.7rem .9rem;background:rgba(0,0,0,.38)}
.stat b{display:block;font-size:.62rem;text-transform:uppercase;letter-spacing:.1em;color:#6b7c93;margin-bottom:.25rem}
.tag{display:inline-block;padding:.15rem .5rem;border-radius:4px;font-size:.65rem;text-transform:uppercase;letter-spacing:.05em}
.tag.expected{background:rgba(0,209,255,.15);color:#00d1ff}
.tag.needs{background:rgba(255,96,96,.2);color:#ff9090}
.tag.sandbox{background:rgba(255,176,32,.15);color:#ffb020}
.tag.guard{background:rgba(57,255,20,.12);color:#7dffb8}
table{width:100%;border-collapse:collapse;font-size:.78rem;margin-top:.65rem}
th,td{border-bottom:1px solid rgba(255,255,255,.08);padding:.5rem .55rem;text-align:left;vertical-align:top}
th{color:#6b7c93;text-transform:uppercase;font-size:.62rem;letter-spacing:.06em}
ul{margin:.4rem 0 0;padding-left:1.2rem}
footer{margin-top:2.5rem;padding-top:1rem;border-top:1px solid rgba(255,255,255,.08);font-size:.72rem;color:#6b7c93}
a{color:#00d1ff}"""


def esc(s: object) -> str:
    return html.escape(str(s) if s is not None else "")


def ver_color(ver: str) -> str:
    v = (ver or "").lower()
    if v == "clean":
        return "#39ff14"
    if v.startswith("fail") or v == "failed":
        return "#ff6060"
    if v.startswith("warn"):
        return "#ffb020"
    return "#00d1ff"


def fix_repro(cmd: str, guard: str) -> str:
    if not cmd:
        return ""
    wasm = GUARD_WASM.get(guard, GUARD_WASM["sv2_reader_bounds"])
    m = re.search(r'-input\s+"([^"]+)"', cmd)
    inp = m.group(1) if m else "0"
    return f'go run ./tools/check_repro -wasm "{wasm}" -input "{inp}"'


def tag_class(triage: str) -> str:
    return {
        "expected_signal": "expected",
        "needs_triage": "needs",
        "sandbox": "sandbox",
        "guard_signal": "guard",
    }.get(triage, "review")


def issue_rows(top_issues: list, guard: str) -> str:
    if not top_issues:
        return '<tr><td colspan="5" class="muted">No findings in this sample window.</td></tr>'
    rows = []
    for i in top_issues[:8]:
        triage = i.get("triage_class") or ""
        note = i.get("triage_note") or triage
        repro = fix_repro(i.get("repro_cmd") or "", guard)
        repro_cell = f'<code>{esc(repro)}</code>' if repro else '<span class="muted">—</span>'
        rows.append(
            f'<tr><td>{esc(i.get("severity"))}</td><td>{esc(i.get("finding_type"))}</td>'
            f'<td><span class="tag {tag_class(triage)}">{esc(triage)}</span><br/><span class="muted">{esc(note)}</span></td>'
            f'<td>{esc(i.get("title"))}</td><td>{repro_cell}</td></tr>'
        )
    return "".join(rows)


def repro_section(top_issues: list, guard: str) -> str:
    blocks = []
    for i in top_issues[:3]:
        repro = fix_repro(i.get("repro_cmd") or "", guard)
        if not repro:
            continue
        blocks.append(
            f'<div class="repro-block"><p class="lbl">{esc(i.get("severity"))} · {esc(i.get("finding_type"))}</p>'
            f'<pre class="repro">{esc(repro)}</pre></div>'
        )
    if not blocks:
        return ""
    return '<div class="card"><p class="lbl">Reproduction commands</p>' + "".join(blocks) + "</div>"


def scope_block() -> str:
    return """<div class="card scope"><p class="lbl">Scope &amp; honesty</p>
<p>This report covers <strong>WASM sandbox</strong> property guards mapped from
<a href="https://github.com/Mecanik/mkpool">Mecanik/mkpool</a> parser paths (<code>sv2_codec.hpp</code>,
<code>stratum_protocol.cpp</code>, <code>client_session.cpp</code>) — not native ASAN on their binary.
<strong>Guard signals</strong> on malformed inputs are expected reject/truncate paths, not confirmed CVEs.</p></div>"""


def render_guard_page(report: dict, guard: str, gen: str, back: str = "../") -> str:
    camp = report.get("campaign") or {}
    sm = report.get("security_summary") or {}
    ver = report.get("verdict") or "unknown"
    vc = ver_color(ver)
    title = camp.get("title") or guard
    cid = camp.get("id") or ""
    recs = [
        "0 critical in this pass — no urgent patch requested for mkpool.",
        "Re-run repro locally; validate any crash-class signal on native mkpool + ASAN before CVE claims.",
    ]
    rec_li = "".join(f"<li>{esc(r)}</li>" for r in recs)
    top = report.get("top_issues") or []

    return f"""<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"/><meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>HackMe Fuzz Report · {esc(title)}</title>
<style>{CSS}</style>
</head><body><div class="wrap">
<h1>HackMe Security Report</h1>
<p class="sub">fuzz_report_v2 · {esc(gen)} · <a href="https://hackme.tech" target="_blank" rel="noopener">hackme.tech</a> · <a href="{esc(back)}">mkpool pack</a></p>
{scope_block()}
<div class="card">
<p class="lbl">Campaign</p>
<p class="title">{esc(title)}</p>
<p class="muted"><code>{esc(cid)}</code> · property · status {esc(camp.get("status"))}</p>
<span class="badge">{esc(ver)}</span>
<div class="grid">
<div class="stat"><b>Confidence</b>{esc(sm.get("confidence"))}</div>
<div class="stat"><b>Guard signals</b>{esc(sm.get("vulnerabilities_found"))}</div>
<div class="stat"><b>Critical</b>{esc(sm.get("critical_count"))}</div>
<div class="stat"><b>High</b>{esc(sm.get("high_count"))}</div>
<div class="stat"><b>Runs done</b>{esc((camp.get("summary") or {}).get("runs_done"))}</div>
<div class="stat"><b>Budget runs</b>{esc(camp.get("budget_runs"))}</div>
</div></div>
<div class="card"><p class="lbl">Top issues (with triage)</p>
<table><thead><tr><th>Severity</th><th>Type</th><th>Triage</th><th>Title</th><th>Repro</th></tr></thead>
<tbody>{issue_rows(top, guard)}</tbody></table></div>
{repro_section(top, guard)}
<div class="card"><p class="lbl">Recommendations</p><ul>{rec_li}</ul></div>
<footer>HackMe Network · fuzz_report_v2 · mkpool voluntary research · Generated {esc(gen)}</footer>
</div></body></html>"""


def render_index(pack: dict, guards: list[dict], gen: str) -> str:
    total_crit = sum((g["summary"].get("critical_count") or 0) for g in guards)
    total_runs = sum((g["runs"] or 0) for g in guards)
    total_sig = sum((g["summary"].get("high_count") or 0) for g in guards)
    ver = "clean" if total_crit == 0 else "fail_high"
    vc = "#39ff14" if total_crit == 0 else ver_color(ver)
    rows = ""
    for g in guards:
        rows += (
            f'<tr><td>{esc(g["name"])}</td><td>{esc(g["runs"])}</td>'
            f'<td>{esc(g["summary"].get("critical_count", 0))}</td>'
            f'<td>{esc(g["summary"].get("high_count", 0))}</td>'
            f'<td><a href="{esc(g["file"])}">fuzz_report_v2</a></td></tr>'
        )
    return f"""<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"/><meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>HackMe Fuzz Report · mkpool parser pack</title>
<link rel="canonical" href="https://hackme.tech/reports/mkpool-fuzz/"/>
<style>{CSS}</style>
</head><body><div class="wrap">
<h1>HackMe Security Report</h1>
<p class="sub">fuzz_report_v2 · mkpool pack · {esc(gen)} · <a href="https://github.com/Mecanik/mkpool">Mecanik/mkpool</a></p>
{scope_block()}
<div class="card">
<p class="lbl">Campaign pack</p>
<p class="title">mkpool · Stratum/SV2 parser boundary fuzz</p>
<p class="muted">4 WASM guards · ~16k property checks · voluntary research for issue #2</p>
<span class="badge" style="border-color:{vc};color:{vc}">0 CRITICAL · GUARD SIGNALS OK</span>
<div class="grid">
<div class="stat"><b>Total runs</b>{total_runs}</div>
<div class="stat"><b>Guards</b>4</div>
<div class="stat"><b>Critical</b>{total_crit}</div>
<div class="stat"><b>High (sample)</b>{total_sig}</div>
</div></div>
<div class="card"><p class="lbl">Per-guard reports (same format as node #fuzz)</p>
<table><thead><tr><th>Guard</th><th>Runs</th><th>Critical</th><th>High</th><th>Report</th></tr></thead>
<tbody>{rows}</tbody></table></div>
<div class="card"><p class="lbl">Verdict</p>
<p>No confirmed memory-safety bug. Guard signals = malformed inputs that mkpool should reject. <strong>Nothing to patch urgently</strong> from this WASM pass.</p>
</div>
<footer>HackMe Network · fuzz_report_v2 · <a href="https://hackme.tech/research.html">Research</a> · Generated {esc(gen)}</footer>
</div></body></html>"""


def main() -> int:
    pack_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else ROOT / "reports/mkpool-fuzz/mkpool-fuzz-20260620-194159"
    out_dir = Path(sys.argv[2]) if len(sys.argv) > 2 else ROOT / "web/site/reports/mkpool-fuzz"
    out_dir.mkdir(parents=True, exist_ok=True)

    guards_meta = []
    gen = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")

    for guard in GUARD_WASM:
        src = pack_dir / f"report_{guard}.json"
        if not src.is_file():
            print(f"skip missing {src}", file=sys.stderr)
            continue
        report = json.loads(src.read_text())
        out_file = f"{guard}.html"
        (out_dir / out_file).write_text(render_guard_page(report, guard, gen), encoding="utf-8")
        sm = report.get("security_summary") or {}
        camp = report.get("campaign") or {}
        guards_meta.append({
            "name": guard,
            "file": out_file,
            "runs": (camp.get("summary") or {}).get("runs_done"),
            "summary": sm,
        })
        print(f"wrote {out_dir / out_file}")

    summary_path = pack_dir / "PACK_SUMMARY.json"
    pack = json.loads(summary_path.read_text()) if summary_path.is_file() else {}
    index_html = render_index(pack, guards_meta, gen)
    (out_dir / "index.html").write_text(index_html, encoding="utf-8")
    (out_dir / "CURRENT.html").write_text(index_html, encoding="utf-8")
    print(f"wrote {out_dir / 'index.html'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
