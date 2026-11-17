"""HackMe Network Pitch — institutional outline."""

from __future__ import annotations

from pathlib import Path

from reportlab.pdfgen import canvas

from . import brand as b


def build(logo: str, glow: str, out: Path) -> Path:
    total = 8
    c = b.new_doc(out, "HackMe Network Pitch", "Institutional pitch · HMC · SUP · HMS")
    _cover(c, logo, glow, total)
    _problem(c, total)
    _solution(c, total)
    _ecosystem(c, total)
    _tech(c, total)
    _tokenomics(c, total)
    _traction(c, total)
    _close(c, logo, total)
    c.save()
    return out


def _cover(c: canvas.Canvas, logo: str, glow: str, total: int):
    b.bookmark(c, "cover", "01 · Cover")
    b.cover_chrome(
        c,
        logo,
        glow,
        "INSTITUTIONAL PITCH",
        "HackMe",
        "Network",
        "Blake3 PoH share-PPS + Dig/Hunt useful-compute pool",
    )
    pills = ["HMC · SUP · HMS", "Own desk first", f"{b.VERSION}", "AGPL-3.0"]
    px = b.MARGIN
    py = b.H - 292
    for p in pills:
        tw = c.stringWidth(p, "Mono", 7) + 18
        c.setFillColorRGB(*b.rgb(b.CARD))
        c.roundRect(px, py, tw, 20, 10, fill=1, stroke=0)
        c.setStrokeColorRGB(*b.rgb(b.LINE))
        c.roundRect(px, py, tw, 20, 10, fill=0, stroke=1)
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.setFont("Mono", 7)
        c.drawString(px + 9, py + 6, p)
        px += tw + 6
    b.cover_footer_card(c)
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Mono", 6.5)
    c.drawCentredString(b.W / 2, 28, f"01  —  COVER    ·    {total} PAGES")
    c.showPage()


def _problem(c: canvas.Canvas, total: int):
    b.fill_bg(c)
    b.bookmark(c, "problem", "02 · Problem")
    b.header_bar(c, 2, total, "PROBLEM")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "01  ·  Problem")
    b.h1(c, b.MARGIN, b.H - 88, "Why this exists", 20)
    items = [
        ("Empty GPU work", "Mining often produces no verifiable customer output."),
        ("Audit accountability", "Security teams need repeatable fuzz with economic stakes."),
        ("Opaque operators", "Many PoW projects lack transparent treasury and settlement discipline."),
    ]
    y = b.H - 130
    for title, desc in items:
        b.card(c, b.MARGIN, y - 70, b.W - 2 * b.MARGIN, 70, radius=10)
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.setFont("MonoB", 8)
        c.drawString(b.MARGIN + 18, y - 20, title.upper())
        b.body(c, desc, b.MARGIN + 18, y - 40, b.W - 2 * b.MARGIN - 36, size=10, leading=14)
        y -= 86
    c.showPage()


def _solution(c: canvas.Canvas, total: int):
    b.fill_bg(c)
    b.bookmark(c, "solution", "03 · Solution")
    b.header_bar(c, 3, total, "SOLUTION")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "02  ·  Solution")
    b.h1(c, b.MARGIN, b.H - 88, "One stack, three layers", 20)
    layers = [
        ("1", "Chain (HMC)", "PoH blocks, transfers, emission, long addresses + chain_id"),
        ("2", "Coordinator pool", "Fair attempt accounting, hybrid signatures, share-PPS"),
        ("3", "Orders / fuzz", "Dig (WASM) + Hunt (ASAN) escrow on the same network"),
        ("4", "Own spot desk", "exchange.hackme.tech paper soft — no futures at start"),
    ]
    y = b.H - 130
    for n, title, desc in layers:
        y = b.numbered_step(c, b.MARGIN, y, n, title, desc, b.W - 2 * b.MARGIN)
        y -= 10
    c.showPage()


def _ecosystem(c: canvas.Canvas, total: int):
    b.fill_bg(c)
    b.bookmark(c, "eco", "04 · Ecosystem")
    b.header_bar(c, 4, total, "ECOSYSTEM")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "03  ·  Ecosystem map")
    b.h1(c, b.MARGIN, b.H - 88, "HMC · SUP · HMS", 20)
    rows = [
        ("Ticker", "Role / status"),
        ("HMC", "Settlement + mining · Live · Economics v2"),
        ("SUP", "Loyalty / fees · Live · cap 21,000"),
        ("HMS", "Storage + seal · Prelaunch · 21M cap"),
    ]
    y = b.table(c, b.MARGIN, b.H - 120, [100, b.W - 2 * b.MARGIN - 100], rows)
    y -= 24
    b.section_label(c, b.MARGIN, y, "Business model")
    y -= 18
    for line in [
        "Miners: HMC + SUP from honest pool work",
        "Customers: prepaid HMC for security audits / fuzz",
        "Long-term: storage lane (HMS) B2B backup revenue",
    ]:
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.circle(b.MARGIN + 5, y + 3, 2.5, fill=1, stroke=0)
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 10)
        c.drawString(b.MARGIN + 16, y, line)
        y -= 18
    c.showPage()


def _tech(c: canvas.Canvas, total: int):
    b.fill_bg(c)
    b.bookmark(c, "tech", "05 · Technology")
    b.header_bar(c, 5, total, "TECH")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "04  ·  Technology proof")
    b.h1(c, b.MARGIN, b.H - 88, "What is already live", 20)
    bullets = [
        f"Live pool + explorer + downloads ({b.VERSION})",
        "Hunt Watch 2026sep: 12/12 CLOSED",
        "OSS CVE Watch: nghttp2 14/14 CLEAN · libheif 14/14 CLEAN",
        "Blake3 PoH · SHARE_STRICT share-PPS · Dig/Hunt escrow",
        "AGPL source on GitHub",
    ]
    y = b.H - 120
    for bl in bullets:
        b.card(c, b.MARGIN, y - 40, b.W - 2 * b.MARGIN, 40, radius=8)
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 10)
        c.drawString(b.MARGIN + 16, y - 24, b.fit_text(c, bl, "Sans", 10, b.W - 2 * b.MARGIN - 32))
        y -= 48
    c.showPage()


def _tokenomics(c: canvas.Canvas, total: int):
    b.fill_bg(c)
    b.bookmark(c, "tok", "06 · Tokenomics")
    b.header_bar(c, 6, total, "TOKENOMICS")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "05  ·  HMC summary")
    b.h1(c, b.MARGIN, b.H - 88, "Economics v2 (live)", 20)
    chips = [
        ("Base", "0.1 HMC", "since #550k"),
        ("Halving", "~4 years", "4,204,800 blk"),
        ("Tail", "0.002", "permanent"),
        ("Burn", "5% real", "orders"),
    ]
    cw = (b.W - 2 * b.MARGIN - 18) / 4
    for i, chip in enumerate(chips):
        b.metric_chip(c, b.MARGIN + i * (cw + 6), b.H - 180, cw, 58, *chip)
    y = b.H - 220
    b.section_label(c, b.MARGIN, y, "SUP & HMS")
    y -= 18
    rows = [
        ("Asset", "Facts"),
        ("SUP", "21,000 cap · 0.002/block · fee asset · CEX after HMC"),
        ("HMS", "21M cap · storage economics · not tradable yet"),
    ]
    b.table(c, b.MARGIN, y, [80, b.W - 2 * b.MARGIN - 80], rows)
    y = b.H - 360
    b.card(c, b.MARGIN, y - 50, b.W - 2 * b.MARGIN, 50, radius=8)
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Sans", 9)
    c.drawString(b.MARGIN + 16, y - 20, "50,000 HMC genesis treasury · public address · no team pre-mine declared")
    b.draw_link(c, "HMC-719006d93916ad52", b.URL["treasury"], b.MARGIN + 16, y - 38, font="MonoM", size=9)
    c.showPage()


def _traction(c: canvas.Canvas, total: int):
    b.fill_bg(c)
    b.bookmark(c, "traction", "07 · Traction & roadmap")
    b.header_bar(c, 7, total, "TRACTION")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "06  ·  Traction — honest")
    b.h1(c, b.MARGIN, b.H - 88, "Now vs later", 20)
    y = b.H - 120
    b.card(c, b.MARGIN, y - 120, (b.W - 2 * b.MARGIN - 12) / 2, 120, radius=10)
    b.card(c, b.MARGIN + (b.W - 2 * b.MARGIN - 12) / 2 + 12, y - 120, (b.W - 2 * b.MARGIN - 12) / 2, 120, radius=10)
    half = (b.W - 2 * b.MARGIN - 12) / 2
    c.setFillColorRGB(*b.rgb(b.CYAN))
    c.setFont("MonoB", 8)
    c.drawString(b.MARGIN + 16, y - 20, "NOW")
    c.drawString(b.MARGIN + half + 28, y - 20, "LATER")
    c.setFillColorRGB(*b.rgb(b.SOFT))
    c.setFont("Sans", 8.5)
    now = ["Own spot desk (paper)", "Miners + Dig/Hunt customers", "Public pool + research ledgers", "No CEX volume yet"]
    later = ["External PoW-friendly CEX", "MM when budget exists", "SUP companion listing", "Tier-1 after legal + volume"]
    for i, line in enumerate(now):
        c.drawString(b.MARGIN + 16, y - 44 - i * 16, "· " + line)
    for i, line in enumerate(later):
        c.drawString(b.MARGIN + half + 28, y - 44 - i * 16, "· " + line)
    y -= 150
    b.section_label(c, b.MARGIN, y, "Risks")
    y -= 16
    for r in [
        "Single canonical hub (decentralization narrative evolving)",
        "Early-stage liquidity",
        "Regulatory classification varies by jurisdiction",
    ]:
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 9)
        c.drawString(b.MARGIN, y, "·  " + r)
        y -= 16
    c.showPage()


def _close(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "close", "08 · Ask")
    b.header_bar(c, 8, total, "CLOSE")
    b.footer_bar(c)
    b.section_label(c, b.MARGIN, b.H - 58, "07  ·  Next step")
    b.h1(c, b.MARGIN, b.H - 88, "Verify, then talk", 20)
    links = [
        ("Site", "hackme.tech", b.URL["home"]),
        ("Listing hub", "listing.html", b.URL["listing"]),
        ("Economics", "economics-model.html", b.URL["economics"]),
        ("Contacts", "contacts.html", b.URL["contacts"]),
        ("GitHub", "jokeez/hackme", b.URL["github"]),
        ("Release", b.VERSION, b.URL["release"]),
    ]
    y = b.H - 130
    card_w = (b.W - 2 * b.MARGIN - 12) / 2
    for i in range(0, len(links), 2):
        for j in range(2):
            if i + j >= len(links):
                break
            lab, val, url = links[i + j]
            x = b.MARGIN + j * (card_w + 12)
            b.card(c, x, y - 48, card_w, 48, radius=8)
            b.link_box(c, url, x, y - 48, card_w, 48)
            c.setFillColorRGB(*b.rgb(b.MUTED))
            c.setFont("Mono", 6.5)
            c.drawString(x + 14, y - 14, lab.upper())
            c.setFillColorRGB(*b.rgb(b.CYAN))
            c.setFont("MonoM", 9)
            c.drawString(x + 14, y - 32, val)
        y -= 58
    b.card(c, b.MARGIN, 60, b.W - 2 * b.MARGIN, 70, radius=12)
    c.drawImage(logo, b.MARGIN + 18, 72, width=44, height=44, mask="auto")
    c.setFillColorRGB(*b.rgb(b.TEXT))
    c.setFont("SansB", 13)
    c.drawString(b.MARGIN + 78, 100, "Not investment advice")
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Sans", 8.5)
    c.drawString(b.MARGIN + 78, 80, f"HackMe Network · {b.PACK_DATE_LONG}")
    c.showPage()
