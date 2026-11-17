"""AGPL-3.0 License PDF — branded dark theme."""

from __future__ import annotations

from pathlib import Path

from reportlab.pdfgen import canvas

from . import brand as b


def build(logo: str, glow: str, out: Path) -> Path:
    license_path = b.ROOT / "LICENSE"
    text = license_path.read_text(encoding="utf-8")
    lines = text.splitlines()

    # Cover + body pages
    pages_body = _paginate(lines)
    total = 1 + len(pages_body)
    c = b.new_doc(out, "AGPL-3.0 License — HackMe", "GNU Affero General Public License v3.0")

    # cover
    b.bookmark(c, "cover", "01 · Cover")
    b.cover_chrome(
        c,
        logo,
        glow,
        "SOFTWARE LICENSE",
        "AGPL",
        "3.0",
        "Full license text · HackMe Network source",
    )
    b.card(c, b.MARGIN, b.H - 430, b.W - 2 * b.MARGIN, 120, radius=10)
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Mono", 7)
    c.drawString(b.MARGIN + 18, b.H - 340, "COPYRIGHT NOTICE")
    notice_lines = [ln for ln in lines[:10] if ln.strip()]
    y = b.H - 360
    for ln in notice_lines[:5]:
        y = b.body(c, ln, b.MARGIN + 18, y, b.W - 2 * b.MARGIN - 36, size=8, leading=11, color=b.SOFT) - 2
    b.cover_footer_card(c)
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Mono", 6.5)
    c.drawCentredString(b.W / 2, 28, f"01  —  COVER    ·    {total} PAGES")
    c.showPage()

    for i, page_lines in enumerate(pages_body, start=2):
        b.fill_bg(c)
        b.bookmark(c, f"p{i}", f"{i:02d} · License text")
        b.header_bar(c, i, total, "AGPL-3.0")
        b.footer_bar(c, right="Full AGPL-3.0 text")
        y = b.H - 52
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Mono", 7.2)
        for ln in page_lines:
            if y < 42:
                break
            # wrap long lines
            chunks = _wrap_mono(c, ln if ln else " ", "Mono", 7.2, b.W - 2 * b.MARGIN)
            for ch in chunks:
                if y < 42:
                    break
                c.drawString(b.MARGIN, y, ch)
                y -= 10
        c.showPage()

    c.save()
    return out


def _wrap_mono(c: canvas.Canvas, text: str, font: str, size: float, max_w: float) -> list[str]:
    if not text:
        return [" "]
    if c.stringWidth(text, font, size) <= max_w:
        return [text]
    out = []
    while text:
        cut = len(text)
        while cut > 1 and c.stringWidth(text[:cut], font, size) > max_w:
            cut -= 1
        # prefer break at space
        sp = text.rfind(" ", 0, cut)
        if sp > 20:
            cut = sp
        out.append(text[:cut])
        text = text[cut:].lstrip()
    return out or [" "]


def _paginate(lines: list[str]) -> list[list[str]]:
    # Approx lines per page at 7.2pt / 10 leading from y=H-52 to 42
    usable = b.H - 52 - 42
    per = max(40, int(usable / 10))
    pages = []
    buf: list[str] = []
    # skip blank lead after copyright header already shown on cover — keep full text for legal fidelity
    for ln in lines:
        # estimate wrap expansion for long lines
        approx = max(1, (len(ln) // 95) + 1)
        if len(buf) + approx > per and buf:
            pages.append(buf)
            buf = []
        buf.append(ln)
    if buf:
        pages.append(buf)
    return pages
