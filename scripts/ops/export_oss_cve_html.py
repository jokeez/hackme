#!/usr/bin/env python3
"""Export OSS CVE hunt rollup to public site (HOLD banner when CVE_CANDIDATE)."""
import json
import sys
from pathlib import Path

def main() -> int:
    if len(sys.argv) < 2:
        print("usage: export_oss_cve_html.py REPORT_DIR", file=sys.stderr)
        return 2
    report = Path(sys.argv[1]).resolve()
    rollup_path = report / "ROLLUP.json"
    if not rollup_path.is_file():
        print(f"missing {rollup_path}", file=sys.stderr)
        return 2
    root = report
    for _ in range(8):
        if (root / "web" / "site").is_dir():
            break
        root = root.parent
    else:
        print("web/site not found", file=sys.stderr)
        return 2
    out = root / "web" / "site" / "reports" / "oss-cve"
    out.mkdir(parents=True, exist_ok=True)
    r = json.loads(rollup_path.read_text())
    hold = r.get("verdict") == "CVE_CANDIDATE"
    rows = ""
    for t in r.get("targets", []):
        nc = len(t.get("crashes", []))
        crash_cell = "—" if nc > 0 else "0"
        rows += f"<tr><td>{t.get('target_id')}</td><td>{t.get('iterations')}</td><td>{crash_cell}</td><td>{t.get('verdict')}</td></tr>"
    banner = (
        '<p class="hold"><strong>Responsible disclosure HOLD</strong> — '
        "CVE candidate(s) under maintainer triage. Do not weaponize inputs.</p>"
        if hold
        else '<p class="clean">No ASAN crash in budget — methodology case study.</p>'
    )
    html = f"""<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>OSS CVE Hunt</title>
<style>body{{font-family:system-ui;max-width:920px;margin:2rem auto;padding:0 1rem;line-height:1.5}}
.hold{{background:#fff3cd;border:1px solid #ffc107;padding:1rem;border-radius:6px}}
.clean{{color:#0a0}} table{{border-collapse:collapse;width:100%}}td,th{{border:1px solid #ccc;padding:8px}}</style>
</head><body>
<h1>OSS CVE Hunt — real upstream ASAN</h1>
{banner}
<p>{r.get('summary','')}</p>
<p><code>verdict={r.get('verdict')}</code> · started {r.get('started_at')}</p>
<h2>Targets</h2>
<table><tr><th>ID</th><th>Iterations</th><th>Signals</th><th>Verdict</th></tr>
{rows}
</table>
<p><a href="https://github.com/jokeez/hackme/blob/main/docs/OSS_CVE_HUNT.md">Methodology</a></p>
</body></html>"""
    (out / "index.html").write_text(html)
    meta = {
        "verdict": r.get("verdict"),
        "summary": r.get("summary"),
        "cve_candidates": r.get("cve_candidates", []),
        "clean_targets": r.get("clean_targets", []),
        "publish_allowed": not hold,
    }
    (out / "meta.json").write_text(json.dumps(meta, indent=2) + "\n")
    run_html = html.replace("</body></html>", "").replace(
        "<h1>OSS CVE Hunt — real upstream ASAN</h1>",
        "<h1>OSS CVE Hunt — latest run</h1><p><a href=\"./index.html\">← Case studies</a></p>",
    ) + "</body></html>"
    (out / "latest-run.html").write_text(run_html)
    cases_script = root / "scripts" / "ops" / "export_oss_cve_cases.py"
    if cases_script.is_file():
        import subprocess
        subprocess.run([sys.executable, str(cases_script), str(root)], check=False)
    print(f"exported → {out}")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
