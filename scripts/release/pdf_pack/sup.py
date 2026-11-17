"""SUP Companion Overview — Economics v2 truth."""

from __future__ import annotations

from pathlib import Path

from reportlab.pdfgen import canvas

from . import brand as b


def build(logo: str, glow: str, out: Path) -> Path:
    total = 6
    c = b.new_doc(out, "SUP Companion Overview — HackMe Network", "SUP utility · emission · listing policy")
    _cover(c, logo, glow, total)
    _what(c, logo, total)
    _supply(c, logo, total)
    _utility(c, logo, total)
    _policy(c, logo, total)
    _links(c, logo, total)
    c.save()
    return out


def _cover(c: canvas.Canvas, logo: str, glow: str, total: int):
    b.bookmark(c, "cover", "01 · Cover")
    b.cover_chrome(
        c,
        logo,
        glow,
        "COMPANION ASSET OVERVIEW",
        "SUP",
        "Companion Pack",
        "HackMe Support · loyalty utility · fee asset · never Dig/Hunt escrow",
    )
    pills = ["Cap 21,000", "0.002 SUP/block", "Fee asset only", f"Channel {b.VERSION}"]
    px, py = b.MARGIN, b.H - 292
    for p in pills:
        tw = c.stringWidth(p, "Mono", 7) + 18
        c.setFillColorRGB(*b.rgb(b.CARD))
        c.roundRect(px, py, tw, 20, 10, fill=1, stroke=0)
        c.setStrokeColorRGB(*b.rgb(b.LINE))
        c.setLineWidth(0.7)
        c.roundRect(px, py, tw, 20, 10, fill=0, stroke=1)
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.setFont("Mono", 7)
        c.drawString(px + 9, py + 6, p)
        px += tw + 6

    b.section_label(c, b.MARGIN, b.H - 340, "Contents")
    toc = [
        ("02", "What SUP is", "what"),
        ("03", "Supply & emission", "supply"),
        ("04", "Utility & rules", "utility"),
        ("05", "Listing policy", "policy"),
        ("06", "Links", "links"),
    ]
    y = b.H - 362
    for num, title, dest in toc:
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.setFont("MonoB", 8)
        c.drawString(b.MARGIN, y, num)
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 9)
        c.drawString(b.MARGIN + 28, y, title)
        c.linkRect("", dest, (b.MARGIN, y - 4, b.W - b.MARGIN, y + 12), relative=0, thickness=0)
        y -= 18
    b.cover_footer_card(c)
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Mono", 6.5)
    c.drawCentredString(b.W / 2, 28, f"01  —  COVER    ·    {total} PAGES")
    c.showPage()


def _what(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "what", "02 · What SUP is")
    b.header_bar(c, 2, total, "SUP")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "01  ·  Positioning")
    b.h1(c, b.MARGIN, b.H - 88, "HackMe Support — SUP", 20)
    y = b.body(
        c,
        "SUP is the loyalty / utility companion to HMC. It rewards honest pool work and "
        "can pay non-refundable order fees. Escrow for Dig/Hunt always stays HMC. "
        "Economics v2 has been live since block 550,000.",
        b.MARGIN,
        b.H - 112,
        b.W - 2 * b.MARGIN,
        size=9.5,
        leading=14,
    )
    y -= 16
    chips = [
        ("Role", "Companion", "Not primary money"),
        ("Cap", "21,000", "Since #550,000"),
        ("Emit", "0.002 / block", "No permanent tail"),
        ("Fee use", "Order fees", "Escrow = HMC"),
    ]
    cw = (b.W - 2 * b.MARGIN - 18) / 4
    for i, (lab, val, sub) in enumerate(chips):
        b.metric_chip(c, b.MARGIN + i * (cw + 6), y - 58, cw, 58, lab, val, sub)
    y -= 90
    b.section_label(c, b.MARGIN, y, "Vs HMC")
    y -= 18
    rows = [
        ("", "HMC", "SUP"),
        ("Role", "Settlement + escrow", "Loyalty + fees"),
        ("Supply", "No hard cap (v2)", "21,000 hard cap"),
        ("Earn", "Blake3 PoH / orders", "Honest HMC pool work"),
        ("Dig/Hunt escrow", "Yes", "Never"),
    ]
    # simple 3-col table
    col = [120, (b.W - 2 * b.MARGIN - 120) / 2, (b.W - 2 * b.MARGIN - 120) / 2]
    for i, row in enumerate(rows):
        yy = y - i * 22
        if i == 0:
            c.setFillColorRGB(*b.rgb(b.CARD2))
            c.roundRect(b.MARGIN, yy - 4, sum(col), 24, 4, fill=1, stroke=0)
            c.setFillColorRGB(*b.rgb(b.CYAN))
            c.setFont("MonoB", 7)
        else:
            if i % 2 == 0:
                c.setFillColorRGB(*b.rgb(b.SURFACE))
                c.rect(b.MARGIN, yy - 4, sum(col), 22, fill=1, stroke=0)
            c.setFillColorRGB(*b.rgb(b.MUTED if i else b.CYAN))
            c.setFont("Sans", 8)
        c.drawString(b.MARGIN + 10, yy + 4, row[0])
        c.setFillColorRGB(*b.rgb(b.TEXT if i else b.CYAN))
        c.setFont("MonoM" if i else "MonoB", 7.5 if i else 7)
        c.drawString(b.MARGIN + col[0] + 8, yy + 4, row[1])
        c.drawString(b.MARGIN + col[0] + col[1] + 8, yy + 4, row[2])
    c.showPage()


def _supply(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "supply", "03 · Supply")
    b.header_bar(c, 3, total, "SUP SUPPLY")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "02  ·  Supply & emission")
    b.h1(c, b.MARGIN, b.H - 88, "Capped loyalty emission", 20)
    y = b.H - 120
    rows = [
        ("Parameter", "Value"),
        ("Max supply (v2)", "21,000 SUP"),
        ("Pre-v2 mint", "Counts toward cap (~3.3k)"),
        ("Block emission", "0.002 SUP / empty PoH block"),
        ("Halving", "Same heights as HMC (4,204,800)"),
        ("Tail", "None — emission stops at cap"),
        ("Genesis airdrop", "None"),
        ("API", "GET /api/sup/economics"),
    ]
    y = b.table(c, b.MARGIN, y, [180, b.W - 2 * b.MARGIN - 180], rows, links={7: b.URL["sup_econ"]})
    y -= 20
    b.card(c, b.MARGIN, y - 70, b.W - 2 * b.MARGIN, 70, radius=8)
    b.body(
        c,
        "v1 used a 21,000,000 ceiling with pool-quality accrual. v2 replaced that with "
        "block emission and a 21,000 hard cap. Everything minted before #550,000 still counts.",
        b.MARGIN + 14,
        y - 18,
        b.W - 2 * b.MARGIN - 28,
        size=8.5,
        leading=12,
    )
    c.showPage()


def _utility(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "utility", "04 · Utility")
    b.header_bar(c, 4, total, "UTILITY")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "03  ·  Utility")
    b.h1(c, b.MARGIN, b.H - 88, "What SUP can do", 20)
    y = b.H - 120
    items = [
        ("Order fee payable in SUP", "Live since #550,000 — escrow always HMC"),
        ("On-chain transfers", "Live (transfer_sup / settle path)"),
        ("Support-miner reputation", "Explorer / dashboard tier"),
        ("Governance signal", "Planned non-binding param votes"),
        ("NOT Dig/Hunt escrow", "Hard rule — never"),
        ("NOT security-audit discount", "Dropped in v2"),
    ]
    for title, desc in items:
        b.card(c, b.MARGIN, y - 48, b.W - 2 * b.MARGIN, 48, radius=8)
        c.setFillColorRGB(*b.rgb(b.TEXT))
        c.setFont("SansB", 10)
        c.drawString(b.MARGIN + 16, y - 18, title)
        c.setFillColorRGB(*b.rgb(b.MUTED))
        c.setFont("Sans", 8.5)
        c.drawString(b.MARGIN + 16, y - 36, desc)
        y -= 56
    c.showPage()


def _policy(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "policy", "05 · Listing policy")
    b.header_bar(c, 5, total, "POLICY")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "04  ·  Listing policy")
    b.h1(c, b.MARGIN, b.H - 88, "Companion after HMC", 20)
    y = b.body(
        c,
        "CEX/DEX outreach for SUP starts only after primary HMC has traction on the own desk "
        "and/or an external venue. SUP is not a standalone airdrop narrative.",
        b.MARGIN,
        b.H - 112,
        b.W - 2 * b.MARGIN,
        size=9.5,
        leading=14,
    )
    y -= 20
    steps = [
        ("HMC desk / CEX first", "Primary asset settles mining and audits."),
        ("Prove SUP fee utility", "Real fee volume in SUP, not vanity mint."),
        ("Companion listing", "Same venues or smaller PoW-friendly desks."),
    ]
    for i, (t, d) in enumerate(steps, 1):
        y = b.numbered_step(c, b.MARGIN, y, i, t, d, b.W - 2 * b.MARGIN)
        y -= 8
    y -= 10
    b.card(c, b.MARGIN, y - 56, b.W - 2 * b.MARGIN, 56, radius=8)
    c.setFillColorRGB(*b.rgb(b.GOLD))
    c.setFont("MonoB", 7.5)
    c.drawString(b.MARGIN + 16, y - 16, "HONEST")
    c.setFillColorRGB(*b.rgb(b.SOFT))
    c.setFont("Sans", 9)
    c.drawString(b.MARGIN + 16, y - 36, "No ROI promises. Price is market-driven. Not investment advice.")
    c.showPage()


def _links(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "links", "06 · Links")
    b.header_bar(c, 6, total, "LINKS")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "05  ·  Verify")
    b.h1(c, b.MARGIN, b.H - 88, "Links", 20)
    links = [
        ("SUP page", "hackme.tech/coin-sup.html", b.URL["coin_sup"]),
        ("SUP economics API", "GET /api/sup/economics", b.URL["sup_econ"]),
        ("HMC listing pack context", "hackme.tech/coin-hmc.html", b.URL["coin"]),
        ("Economics v2 RFC", "RFC_ECONOMICS_V2.md", b.URL["rfc_v2"]),
        ("Transparency", "token-transparency.html", b.URL["transparency"]),
        ("Release", b.VERSION, b.URL["release"]),
    ]
    y = b.H - 120
    for lab, val, url in links:
        b.card(c, b.MARGIN, y - 44, b.W - 2 * b.MARGIN, 44, radius=8)
        b.link_box(c, url, b.MARGIN, y - 44, b.W - 2 * b.MARGIN, 44)
        c.setFillColorRGB(*b.rgb(b.MUTED))
        c.setFont("Mono", 6.5)
        c.drawString(b.MARGIN + 16, y - 14, lab.upper())
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.setFont("MonoM", 9)
        c.drawString(b.MARGIN + 16, y - 30, val)
        y -= 52
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Mono", 7)
    c.drawCentredString(b.W / 2, 48, f"END  ·  SUP Companion  ·  {b.PACK_DATE_LONG}")
    c.showPage()
