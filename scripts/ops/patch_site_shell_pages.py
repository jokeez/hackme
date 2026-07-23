#!/usr/bin/env python3
"""Wire site-shell.js nav/footer on static pages under web/site/."""
from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SITE = ROOT / "web" / "site"

PAGE_MAP = {
    "index.html": "home",
    "coins.html": "coins",
    "downloads.html": "downloads",
    "fuzz-campaigns.html": "fuzz",
    "fuzz-guide.html": "fuzz",
    "fuzz-marketplace.html": "fuzz",
    "research.html": "research",
    "developers.html": "developers",
    "docs.html": "docs",
    "news.html": "news",
    "economics-model.html": "economics",
    "contacts.html": "contacts",
    "legal.html": "legal",
    "legal-privacy.html": "privacy",
    "legal-risk.html": "legal",
    "legal-eula.html": "legal",
    "legal-terms.html": "legal",
    "security-notes.html": "docs",
    "security-rewards.html": "docs",
    "api-reference.html": "developers",
    "developer-console.html": "developers",
    "developer-dashboard.html": "developers",
    "fuzzing-console.html": "fuzz",
    "phasing-console.html": "developers",
    "explorer-lite.html": "home",
}

NAV_RE = re.compile(
    r'<nav class="nav"[^>]*>.*?</nav>',
    re.DOTALL | re.IGNORECASE,
)
FOOTER_RE = re.compile(
    r'(<div class="footer-nav"[^>]*>)(.*?)(</div>)',
    re.DOTALL | re.IGNORECASE,
)
SHELL_TAG = '<script src="./assets/site-shell.js?v=20260601hub"></script>'


def page_key(path: Path) -> str:
    return PAGE_MAP.get(path.name, path.stem.replace("-", "_"))


def patch_file(path: Path) -> bool:
    text = path.read_text(encoding="utf-8")
    orig = text
    key = page_key(path)
    if 'data-site-page="' not in text:
        text, n = NAV_RE.subn(f'<nav class="nav" data-site-page="{key}"></nav>', text, count=1)
        if n == 0:
            return False
    text = FOOTER_RE.sub(r"\1\3", text, count=1)
    if SHELL_TAG not in text:
        if '<script src="./assets/app.js' in text:
            text = text.replace(
                '<script src="./assets/app.js',
                SHELL_TAG + "\n  " + '<script src="./assets/app.js',
                1,
            )
        elif "</body>" in text:
            text = text.replace("</body>", "  " + SHELL_TAG + "\n</body>", 1)
    if text != orig:
        path.write_text(text, encoding="utf-8")
        return True
    return False


def main() -> None:
    changed = []
    for path in sorted(SITE.rglob("*.html")):
        if "reports/" in path.as_posix():
            continue
        if patch_file(path):
            changed.append(path.relative_to(SITE))
    print(f"patched {len(changed)} files")
    for p in changed:
        print(" ", p)


if __name__ == "__main__":
    main()
