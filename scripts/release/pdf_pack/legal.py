"""HackMe Legal and Rights pack."""

from __future__ import annotations

from pathlib import Path

from reportlab.pdfgen import canvas

from . import brand as b


def build(logo: str, glow: str, out: Path) -> Path:
    total = 6
    c = b.new_doc(out, "HackMe Legal and Rights", "Rights · disclosures · trademark · forking")
    _cover(c, logo, glow, total)
    _license(c, total)
    _trademark(c, total)
    _tokens(c, total)
    _live(c, total)
    _risk(c, total)
    c.save()
    return out


def _cover(c: canvas.Canvas, logo: str, glow: str, total: int):
    b.bookmark(c, "cover", "01 · Cover")
    b.cover_chrome(
        c,
        logo,
        glow,
        "LEGAL & RIGHTS",
        "HackMe",
        "Legal Pack",
        "AGPL-3.0 · trademark · disclosures · not investment advice",
    )
    b.cover_footer_card(c)
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Mono", 6.5)
    c.drawCentredString(b.W / 2, 28, f"01  —  COVER    ·    {total} PAGES")
    c.showPage()


def _license(c: canvas.Canvas, total: int):
    b.fill_bg(c)
    b.bookmark(c, "license", "02 · License")
    b.header_bar(c, 2, total, "LICENSE")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "01  ·  Software license")
    b.h1(c, b.MARGIN, b.H - 88, "AGPL-3.0", 20)
    y = b.body(
        c,
        "HackMe node, coordinator, worker binaries, and published tooling are free software "
        "under the GNU Affero General Public License v3.0.",
        b.MARGIN,
        b.H - 112,
        b.W - 2 * b.MARGIN,
        size=9.5,
        leading=14,
    )
    y -= 16
    half = (b.W - 2 * b.MARGIN - 12) / 2
    b.card(c, b.MARGIN, y - 160, half, 160, radius=10)
    b.card(c, b.MARGIN + half + 12, y - 160, half, 160, radius=10)
    c.setFillColorRGB(*b.rgb(b.CYAN))
    c.setFont("MonoB", 8)
    c.drawString(b.MARGIN + 16, y - 20, "YOU MAY")
    c.drawString(b.MARGIN + half + 28, y - 20, "YOU MUST")
    c.setFillColorRGB(*b.rgb(b.SOFT))
    c.setFont("Sans", 8.5)
    may = ["Read, study, audit", "Fork and modify", "Run your own pool/node", "Charge for support"]
    must = ["Keep copyright notices", "Offer corresponding source", "Honor AGPL network use", "Not misrepresent authorship"]
    for i, line in enumerate(may):
        c.drawString(b.MARGIN + 16, y - 44 - i * 22, "· " + line)
    for i, line in enumerate(must):
        c.drawString(b.MARGIN + half + 28, y - 44 - i * 22, "· " + line)
    y -= 180
    b.draw_link(c, "Full text: github.com/jokeez/hackme/LICENSE", b.URL["license"], b.MARGIN, y, font="MonoM", size=8.5)
    y -= 16
    b.draw_link(c, "Also in this pack: AGPL-3.0_License.pdf", b.URL["downloads"], b.MARGIN, y, font="MonoM", size=8.5)
    c.showPage()


def _trademark(c: canvas.Canvas, total: int):
    b.fill_bg(c)
    b.bookmark(c, "tm", "03 · Trademark")
    b.header_bar(c, 3, total, "TRADEMARK")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "02  ·  Brand")
    b.h1(c, b.MARGIN, b.H - 88, "What is not open source", 20)
    y = b.body(
        c,
        "The HackMe name, HackMe Network branding, logos, and hackme.tech are operator assets — "
        "not a blanket license for third parties.",
        b.MARGIN,
        b.H - 112,
        b.W - 2 * b.MARGIN,
        size=9.5,
        leading=14,
    )
    y -= 20
    rows = [
        ("Allowed", "Not allowed"),
        ("Fork under a new name", "Impersonate official pool/site"),
        ("Attribute HackMe as upstream", "Reuse logo/name as competing official"),
        ("Link official downloads + checksums", "Phishing domains mimicking hackme.tech"),
    ]
    y = b.table(c, b.MARGIN, y, [(b.W - 2 * b.MARGIN) / 2, (b.W - 2 * b.MARGIN) / 2], rows)
    y -= 24
    b.section_label(c, b.MARGIN, y, "Operator moat")
    y -= 16
    for line in [
        "Official domain hackme.tech",
        "Production coordinator + canonical node",
        "Community trust, checksums, signed releases",
        "Secrets (.env, settlement keys) never in git",
    ]:
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 9.5)
        c.drawString(b.MARGIN, y, "·  " + line)
        y -= 16
    c.showPage()


def _tokens(c: canvas.Canvas, total: int):
    b.fill_bg(c)
    b.bookmark(c, "tokens", "04 · Tokens")
    b.header_bar(c, 4, total, "TOKENS")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "03  ·  Ecosystem tokens")
    b.h1(c, b.MARGIN, b.H - 88, "Utility, not investment products", 20)
    rows = [
        ("Ticker", "Role / status"),
        ("HMC", "Primary · mining + escrow · Live · Economics v2"),
        ("SUP", "Loyalty / fees · Live · cap 21,000"),
        ("HMS", "Storage · Prelaunch · not tradable yet"),
    ]
    y = b.table(c, b.MARGIN, b.H - 120, [80, b.W - 2 * b.MARGIN - 80], rows)
    y -= 20
    for line in [
        "Tokens are utility / infrastructure instruments — not deposits or securities as described by operators.",
        "No ROI promises in official channels.",
        "SUP listing planned after HMC — not guaranteed.",
        "HMS must not be traded until genesis is announced.",
    ]:
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.circle(b.MARGIN + 5, y + 3, 2.5, fill=1, stroke=0)
        y = b.body(c, line, b.MARGIN + 16, y, b.W - 2 * b.MARGIN - 20, size=9, leading=12) - 8
    y -= 8
    b.draw_link(c, "Live supply → token-transparency.html", b.URL["transparency"], b.MARGIN, y, font="MonoM", size=8.5)
    c.showPage()


def _live(c: canvas.Canvas, total: int):
    b.fill_bg(c)
    b.bookmark(c, "live", "05 · Live snapshot")
    b.header_bar(c, 5, total, "LIVE")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "04  ·  Honest snapshot")
    b.h1(c, b.MARGIN, b.H - 88, f"Channel {b.VERSION}", 20)
    rows = [
        ("Surface", "Status"),
        ("Public pool + coordinator", "Live"),
        ("HMC explorer + /api/status", "Live"),
        ("SUP economics API", "Live"),
        ("Economics v2 @ #550,000", "Live"),
        ("Own spot desk", "Paper soft / D0"),
        ("First PoW CEX listing", "Not yet"),
        ("Market maker", "Not engaged"),
        ("Tier-1 legal entity", "In progress"),
        ("HMS storage lane", "Prelaunch"),
    ]
    b.table(c, b.MARGIN, b.H - 120, [220, b.W - 2 * b.MARGIN - 220], rows)
    c.showPage()


def _risk(c: canvas.Canvas, total: int):
    b.fill_bg(c)
    b.bookmark(c, "risk", "06 · Risk")
    b.header_bar(c, 6, total, "RISK")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "05  ·  Risk & acknowledgment")
    b.h1(c, b.MARGIN, b.H - 88, "Read before participating", 20)
    risks = [
        "No guaranteed profitability",
        "Availability / outage risk",
        "Software defects possible",
        "Credential / misconfiguration loss",
        "Regulatory obligations vary by jurisdiction",
        "Multi-coin complexity (HMC / SUP / HMS)",
    ]
    y = b.H - 120
    for r in risks:
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 10)
        c.drawString(b.MARGIN, y, "·  " + r)
        y -= 18
    y -= 10
    b.card(c, b.MARGIN, y - 100, b.W - 2 * b.MARGIN, 100, radius=10)
    c.setFillColorRGB(*b.rgb(b.GOLD))
    c.setFont("MonoB", 7.5)
    c.drawString(b.MARGIN + 16, y - 18, "ACKNOWLEDGMENT")
    b.body(
        c,
        "By downloading software, mining, or using hackme.tech you accept responsibility for "
        "hardware, wallets, and compliance. Public materials are technical documentation — "
        "not financial, legal, or tax advice.",
        b.MARGIN + 16,
        y - 38,
        b.W - 2 * b.MARGIN - 32,
        size=8.5,
        leading=12,
    )
    y -= 120
    b.draw_link(c, "hackme.tech/legal-risk.html", b.URL["legal"], b.MARGIN, y, font="MonoM", size=9)
    y -= 16
    b.draw_link(c, "hackme.tech/legal-privacy.html", b.URL["privacy"], b.MARGIN, y, font="MonoM", size=9)
    y -= 16
    b.draw_link(c, "hackme.tech/contacts.html", b.URL["contacts"], b.MARGIN, y, font="MonoM", size=9)
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Mono", 7)
    c.drawCentredString(b.W / 2, 48, f"END  ·  Legal Pack  ·  {b.PACK_DATE_LONG}")
    c.showPage()
