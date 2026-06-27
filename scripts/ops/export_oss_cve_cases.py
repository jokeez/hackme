#!/usr/bin/env python3
"""Export OSS CVE case studies to web/site/reports/oss-cve/ (public-safe)."""
from __future__ import annotations

import html
import json
import sys
from pathlib import Path

STATUS_ORDER = ("published", "fixed", "triage", "hold", "closed_wontfix", "pipeline")
STATUS_BADGE = {
    "published": ("published", "#39ff14", "Full case study"),
    "fixed": ("fixed", "#00d1ff", "Fix merged — publishing soon"),
    "triage": ("triage", "#ffc107", "Awaiting maintainer"),
    "hold": ("hold", "#ff9800", "Responsible disclosure hold"),
    "closed_wontfix": ("wontfix", "#8ea3c2", "Upstream wontfix — not CVE-class"),
    "pipeline": ("pipeline", "#6b8099", "In fuzz pipeline"),
}


def esc(s: str) -> str:
    return html.escape(s or "", quote=True)


def repo_root_from(start: Path) -> Path:
    root = start.resolve()
    for _ in range(8):
        if (root / "web" / "site").is_dir():
            return root
        root = root.parent
    raise SystemExit("web/site not found")


def case_card(c: dict) -> str:
    slug = c["slug"]
    status = c.get("status", "hold")
    _, color, label = STATUS_BADGE.get(status, STATUS_BADGE["hold"])
    cve = c.get("cve_id")
    cve_line = (
        f'<p class="cve-id"><strong>CVE:</strong> <a href="https://nvd.nist.gov/vuln/detail/{esc(cve)}">{esc(cve)}</a></p>'
        if cve and status == "published"
        else ""
    )
    return f"""<article class="case-card status-{esc(status)}">
  <div class="case-head">
    <span class="badge" style="border-color:{color};color:{color}">{esc(label)}</span>
    <span class="tier">{esc(c.get("tier", "oss_cve"))}</span>
  </div>
  <h2><a href="./cases/{esc(slug)}.html">{esc(c.get("project", slug))}</a></h2>
  <p class="component">{esc(c.get("component", ""))}</p>
  <p class="summary">{esc(c.get("public_summary", ""))}</p>
  {cve_line}
  <p class="meta">{esc(c.get("finding_class", ""))} · wave {esc(c.get("hunt_wave", "—"))}</p>
  <a class="case-link" href="./cases/{esc(slug)}.html">Case page →</a>
</article>"""


def case_page(c: dict, labels: dict) -> str:
    slug = c["slug"]
    status = c.get("status", "hold")
    _, color, label = STATUS_BADGE.get(status, STATUS_BADGE["hold"])
    status_desc = labels.get(status, status)

    timeline = []
    if c.get("reported_at"):
        timeline.append(("Reported", c["reported_at"]))
    if c.get("closed_at"):
        timeline.append(("Upstream closed", c["closed_at"]))
    if c.get("fixed_at"):
        timeline.append(("Fix merged", c["fixed_at"]))
    if c.get("published_at"):
        timeline.append(("Published", c["published_at"]))
    tl_html = ""
    if timeline:
        items = "".join(f"<li><strong>{esc(k)}:</strong> {esc(v)}</li>" for k, v in timeline)
        tl_html = f'<h2>Timeline</h2><ul class="timeline">{items}</ul>'

    repro_block = ""
    if status == "closed_wontfix":
        repro_block = """<div class="hold-box">
  <p><strong>Upstream: wontfix, not CVE-class</strong> — maintainer declined to treat this as a bug
  (classic C API callback pattern). See linked GitHub issue for discussion. No CVE pursuit.</p>
</div>"""
    elif c.get("show_repro") and status == "published":
        repro_block = '<h2>Reproduction</h2><p>See linked advisory for minimized input and upstream commit.</p>'
    else:
        repro_block = """<div class="hold-box">
  <p><strong>Reproduction withheld</strong> — coordinated disclosure. Full PoC, stack traces, and CVE assignment
  appear here after upstream fix or agreed publish date.</p>
</div>"""

    cve_block = ""
    if c.get("cve_id") and status in ("fixed", "published"):
        cve_block = f'<p><strong>CVE:</strong> <code>{esc(c["cve_id"])}</code></p>'
    links = []
    if c.get("repo_url"):
        links.append(f'<a href="{esc(c["repo_url"])}">Upstream repo</a>')
    if c.get("issue_url"):
        links.append(f'<a href="{esc(c["issue_url"])}">Tracker issue</a>')
    if c.get("fix_url"):
        links.append(f'<a href="{esc(c["fix_url"])}">Fix commit/PR</a>')
    links_html = " · ".join(links)

    return f"""<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"/><meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>HackMe · OSS case · {esc(c.get("project", slug))}</title>
<link rel="canonical" href="https://hackme.tech/reports/oss-cve/cases/{esc(slug)}.html"/>
<style>
*{{box-sizing:border-box}}body{{margin:0;font-family:system-ui,sans-serif;background:#070b12;color:#c8d6e8;line-height:1.55}}
.wrap{{max-width:720px;margin:0 auto;padding:2.5rem 1.25rem 4rem}}
h1{{font-size:1.35rem;color:#00d1ff;margin:0 0 .25rem}}
.sub{{color:#6b8099;font-size:.85rem;margin-bottom:1.5rem}}
.badge{{display:inline-block;padding:.35rem .9rem;border-radius:999px;border:2px solid {color};color:{color};font-weight:600;font-size:.8rem}}
.hold-box{{background:rgba(255,152,0,.08);border:1px solid rgba(255,152,0,.35);border-radius:10px;padding:1rem;margin:1.5rem 0}}
.meta{{color:#6b8099;font-size:.9rem}}a{{color:#00d1ff}}.timeline{{padding-left:1.2rem}}
footer{{margin-top:2.5rem;font-size:.75rem;color:#6b8099}}
</style></head><body><div class="wrap">
<p><a href="../index.html">← OSS CVE cases</a></p>
<h1>{esc(c.get("project", slug))}</h1>
<p class="sub">{esc(c.get("component", ""))} · {esc(status_desc)}</p>
<span class="badge">{esc(label)}</span>
<p style="margin-top:1.25rem">{esc(c.get("public_summary", ""))}</p>
<p class="meta">{esc(c.get("finding_class", ""))} · severity {esc(c.get("severity", "—"))} · {esc(c.get("tier", ""))}</p>
{cve_block}
{tl_html}
{repro_block}
<p style="margin-top:1.5rem">{links_html}</p>
<footer>HackMe Network · <a href="https://github.com/jokeez/hackme/blob/main/docs/OSS_CVE_HUNT.md">methodology</a></footer>
</div></body></html>"""


def hub_page(cases: list, labels: dict, meta: dict | None) -> str:
    sorted_cases = sorted(
        cases,
        key=lambda c: (STATUS_ORDER.index(c.get("status", "hold"))
                       if c.get("status", "hold") in STATUS_ORDER else 99,
                       c.get("project", "")),
    )
    cards = "\n".join(case_card(c) for c in sorted_cases)
    in_pipeline = sum(1 for c in cases if c.get("status") in ("hold", "triage"))
    closed = sum(1 for c in cases if c.get("status") == "closed_wontfix")
    published = sum(1 for c in cases if c.get("status") == "published")

    run_note = ""
    if meta:
        run_note = f"""<section class="panel glass oss-run-panel">
  <h2>Latest hunt run (methodology)</h2>
  <p class="subtle">Aggregate only — no weaponized inputs. <a href="./latest-run.html">run table</a></p>
  <p><code>verdict={esc(meta.get("verdict", ""))}</code> · {len(meta.get("clean_targets", []))} clean targets in last batch</p>
</section>"""

    return f"""<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>HackMe · OSS CVE case studies</title>
  <meta name="description" content="Real upstream Tier-D ASAN hunts. Coordinated disclosure pipeline — case status public, PoC after maintainer fix." />
  <link rel="canonical" href="https://hackme.tech/reports/oss-cve/" />
  <link rel="icon" href="/favicon.ico" type="image/x-icon" />
  <link rel="stylesheet" href="../../assets/styles.css?v=20260627banner2" />
  <link rel="preconnect" href="https://fonts.googleapis.com" />
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800&family=Space+Grotesk:wght@500;700&display=swap" rel="stylesheet" />
  <style>
  .oss-hub-hero {{ margin-bottom: 1.5rem; }}
  .oss-hub-stats {{ display: flex; gap: 1rem; flex-wrap: wrap; margin: 1rem 0 1.5rem; font-size: 0.85rem; color: var(--muted); }}
  .oss-hub-stats b {{ color: var(--primary); }}
  .oss-policy {{
    margin: 1rem 0 1.5rem; padding: 1rem 1.15rem; border-radius: 1rem;
    border: 1px solid rgba(255, 200, 87, 0.35); background: rgba(255, 200, 87, 0.06);
    font-size: 0.9rem; color: #d4c4a8; line-height: 1.55;
  }}
  .oss-case-grid {{ display: grid; gap: 1rem; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); }}
  .case-card {{ border: 1px solid rgba(77, 228, 255, 0.22); border-radius: 14px; padding: 1.2rem; background: rgba(0, 0, 0, 0.35); }}
  .case-card h2 {{ font-size: 1.05rem; margin: 0.6rem 0 0.3rem; }}
  .case-card h2 a {{ color: #e8f4ff; text-decoration: none; }}
  .case-card h2 a:hover {{ color: var(--primary); }}
  .case-card .summary {{ font-size: 0.9rem; color: var(--muted); line-height: 1.5; }}
  .case-card .meta {{ font-size: 0.75rem; color: #6b8099; margin-top: 0.75rem; }}
  .case-card .badge {{ display: inline-block; padding: 0.25rem 0.65rem; border-radius: 999px; border: 2px solid; font-size: 0.7rem; font-weight: 600; }}
  .case-card .tier {{ float: right; font-size: 0.7rem; color: #6b8099; }}
  .case-card .case-link {{ display: inline-block; margin-top: 0.75rem; font-size: 0.82rem; font-weight: 600; color: var(--primary); }}
  .oss-run-panel {{ margin-top: 2rem; }}
  </style>
</head>
<body>
  <div class="bg-glow bg-glow-a" aria-hidden="true"></div>
  <div class="bg-glow bg-glow-b" aria-hidden="true"></div>
  <header class="topbar">
    <a class="brand" href="../../index.html">
      <img class="brand-logo" src="../../assets/logo-hex.png" alt="HackMe logo" />
      <span>HackMe Network</span>
    </a>
    <nav class="nav" data-site-page="research"></nav>
  </header>

  <main class="container">
    <section class="hero glass hero-small oss-hub-hero">
      <p class="kicker">Tier-D · coordinated disclosure</p>
      <h1>OSS CVE case studies</h1>
      <p class="hero-copy">Real upstream Tier-D ASAN hunts. Cases move from <em>hold</em> → <em>triage</em> → <em>fixed</em> → <em>published</em>.
        Repro and CVE IDs appear only after coordinated disclosure.</p>
      <div class="oss-policy"><strong>Public policy:</strong> We show pipeline activity and case status now; technical PoC and CVE numbers only after maintainer fix or agreed publish window.</div>
      <div class="oss-hub-stats">
        <span><b>{len(cases)}</b> tracked cases</span>
        <span><b>{in_pipeline}</b> in disclosure</span>
        <span><b>{closed}</b> closed wontfix</span>
        <span><b>{published}</b> published</span>
      </div>
    </section>

    <section class="panel glass">
      <div class="oss-case-grid">
{cards}
      </div>
    </section>
{run_note}
  </main>

  <footer class="footer">
    <p>HackMe Network</p>
    <p class="muted">Ecosystem: mine · fuzz · research · explore.</p>
    <div class="footer-nav"></div>
  </footer>
  <script src="../../assets/site-shell.js?v=20260627banner2"></script>
</body>
</html>"""


def main() -> int:
    root = repo_root_from(Path(sys.argv[1] if len(sys.argv) > 1 else "."))
    cases_path = root / "upstream" / "oss_cve_cases.json"
    data = json.loads(cases_path.read_text())
    cases = data.get("cases", [])
    labels = data.get("status_labels", {})
    out = root / "web" / "site" / "reports" / "oss-cve"
    case_dir = out / "cases"
    case_dir.mkdir(parents=True, exist_ok=True)

    meta = None
    meta_path = out / "meta.json"
    if meta_path.is_file():
        meta = json.loads(meta_path.read_text())

    (out / "cases.json").write_text(json.dumps({
        "updated": data.get("updated"),
        "cases": [{
            "id": c["id"],
            "slug": c["slug"],
            "project": c.get("project"),
            "status": c.get("status"),
            "severity": c.get("severity"),
            "cve_id": c.get("cve_id"),
            "published_at": c.get("published_at"),
        } for c in cases],
    }, indent=2) + "\n")

    for c in cases:
        out_path = case_dir / f"{c['slug']}.html"
        if c.get("custom_page") and out_path.is_file():
            continue
        out_path.write_text(case_page(c, labels))

    (out / "index.html").write_text(hub_page(cases, labels, meta))
    print(f"exported {len(cases)} cases → {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
