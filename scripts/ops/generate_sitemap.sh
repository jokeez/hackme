#!/usr/bin/env bash
# Regenerate web/site/sitemap.xml from public static HTML (indexable pages only).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SITE="$ROOT/web/site"
OUT="$SITE/sitemap.xml"
LASTMOD="${LASTMOD:-$(date -u +%Y-%m-%d)}"
BASE="https://hackme.tech"

python3 - "$SITE" "$OUT" "$BASE" "$LASTMOD" <<'PY'
import pathlib, sys, xml.sax.saxutils as x
site, out, base, lastmod = sys.argv[1:5]
root = pathlib.Path(site)

# Root-level public pages
pages = sorted(root.glob("*.html"))
# Research / report hubs (no redirect-only shells)
report_globs = [
    "reports/bitcoin30*.html",
    "reports/l1-crypto-stack*.html",
    "reports/fuzz-depth-v3.html",
    "reports/bitcoin-core-5module.html",
    "reports/oss-cve/index.html",
]
for pat in report_globs:
    pages.extend(sorted(root.glob(pat)))

skip = {
    "healthz.html",  # probe only
    "developer-console.html",
    "developer-dashboard.html",
}
# Redirect shells — not canonical index targets
skip_names = skip

def priority(path: pathlib.Path) -> str:
    rel = path.relative_to(root).as_posix()
    if rel == "index.html":
        return "1.0"
    if rel in ("downloads.html", "docs.html", "developers.html", "orders.html", "research.html", "coins.html"):
        return "0.9"
    if rel.startswith("reports/bitcoin30"):
        return "0.72"
    if rel.startswith("reports/"):
        return "0.75"
    if rel.startswith("legal"):
        return "0.5"
    return "0.8"

urls = []
for p in pages:
    if p.name in skip_names:
        continue
    rel = p.relative_to(root).as_posix()
    loc = base + "/" + rel if rel != "index.html" else base + "/"
    urls.append((loc, priority(p)))

# Canonical explorer (not /pool/explorer redirect)
urls.append((base + "/explorer-lite.html", "0.85"))

# Dedupe by loc
seen = set()
unique = []
for loc, pri in urls:
    if loc in seen:
        continue
    seen.add(loc)
    unique.append((loc, pri))

lines = [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">',
]
for loc, pri in sorted(unique, key=lambda x: (-float(x[1]), x[0])):
    lines.append("  <url>")
    lines.append(f"    <loc>{x.escape(loc)}</loc>")
    lines.append(f"    <lastmod>{lastmod}</lastmod>")
    lines.append("    <changefreq>weekly</changefreq>")
    lines.append(f"    <priority>{pri}</priority>")
    lines.append("  </url>")
lines.append("</urlset>")
lines.append("")
pathlib.Path(out).write_text("\n".join(lines))
print(f"wrote {out} ({len(unique)} urls)")
PY

chmod +x "$ROOT/scripts/ops/generate_sitemap.sh" 2>/dev/null || true
