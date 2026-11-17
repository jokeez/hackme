"""HMC Listing Pack — Economics v2 truth."""

from __future__ import annotations

from pathlib import Path

from reportlab.pdfgen import canvas

from . import brand as b


def build(logo: str, glow: str, out: Path) -> Path:
    total = 8
    c = b.new_doc(out, "HMC Listing Pack — HackMe Network", "Exchange due diligence · HackMe Coin · Economics v2")
    c.setKeywords("HMC, HackMe, listing, exchange, PoH, SUP, Economics v2")

    _cover(c, logo, glow, total)
    _memo(c, logo, total)
    _asset(c, logo, total)
    _diff(c, logo, total)
    _tokenomics(c, logo, total)
    _allocation(c, logo, total)
    _dd(c, logo, total)
    _links(c, logo, total)

    c.save()
    return out


def _cover(c: canvas.Canvas, logo: str, glow: str, total: int):
    b.bookmark(c, "cover", "01 · Cover")
    b.cover_chrome(
        c,
        logo,
        glow,
        "EXCHANGE DUE DILIGENCE",
        "HMC",
        "Listing Pack",
        "HackMe Coin · Blake3 PoH · useful-compute network · Economics v2 live",
    )
    pills = ["HMC primary", "SUP companion", f"Channel {b.VERSION}", "Own spot desk first"]
    px, py = b.MARGIN, b.H - 292
    for p in pills:
        tw = c.stringWidth(p, "Mono", 7) + 18
        if px + tw > b.W - b.MARGIN:
            px = b.MARGIN
            py -= 26
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
        ("02", "Executive memo", "memo"),
        ("03", "Primary asset — HMC", "asset"),
        ("04", "Differentiation", "edge"),
        ("05", "Tokenomics (v2 live)", "tokenomics"),
        ("06", "Allocation & vesting", "allocation"),
        ("07", "Due diligence checklist", "dd"),
        ("08", "Links & verification", "links"),
    ]
    y = b.H - 362
    for num, title, dest in toc:
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.setFont("MonoB", 8)
        c.drawString(b.MARGIN, y, num)
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 9)
        c.drawString(b.MARGIN + 28, y, title)
        c.setStrokeColorRGB(*b.rgb(b.LINE))
        c.setDash(1, 3)
        c.setLineWidth(0.5)
        tw = c.stringWidth(title, "Sans", 9)
        c.line(b.MARGIN + 32 + tw, y + 3, b.W - b.MARGIN - 24, y + 3)
        c.setDash()
        c.setFillColorRGB(*b.rgb(b.MUTED))
        c.setFont("Mono", 8)
        c.drawRightString(b.W - b.MARGIN, y, num)
        c.linkRect("", dest, (b.MARGIN, y - 4, b.W - b.MARGIN, y + 12), relative=0, thickness=0)
        y -= 16

    b.cover_footer_card(c)
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Mono", 6.5)
    c.drawCentredString(b.W / 2, 28, f"01  —  COVER    ·    {total} PAGES")
    c.showPage()


def _memo(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "memo", "02 · Executive memo")
    b.header_bar(c, 2, total, "MEMO")
    b.footer_bar(c)
    b.draw_hex_watermark(c, logo, b.W - 80, 120, 200, 0.05)

    b.section_label(c, b.MARGIN, b.H - 58, "01  ·  Executive memo")
    b.h1(c, b.MARGIN, b.H - 88, "What HackMe is", 20)
    y = b.body(
        c,
        "HackMe is useful-compute infrastructure: a native Blake3 PoH chain (HMC), "
        "coordinator-backed mining pool, Dig/Hunt security-audit escrow, and a "
        "multi-coin ecosystem (SUP loyalty, HMS storage lane in prelaunch). "
        "Miners prove work through hybrid-signed submits; customers escrow HMC "
        "for fuzz campaigns. Live hybrid GPUs soft-prioritize Dig/Hunt leases "
        "(GHS↔Dig coupling); ASAN/WASM evaluation stays on CPU.",
        b.MARGIN,
        b.H - 112,
        b.W - 2 * b.MARGIN,
        size=9.5,
        leading=14,
    )
    y -= 8
    b.card(c, b.MARGIN, y - 46, b.W - 2 * b.MARGIN, 50, radius=8)
    c.setFillColorRGB(*b.rgb(b.GOLD))
    c.setFont("MonoB", 7.5)
    c.drawString(b.MARGIN + 16, y - 12, "POSITIONING")
    c.setFillColorRGB(*b.rgb(b.TEXT))
    c.setFont("Sans", 9)
    c.drawString(b.MARGIN + 16, y - 30, "Not a classic Stratum SHA256 pool or an empty GPU coin.")

    y -= 72
    b.section_label(c, b.MARGIN, y, "Listing sequence — honest")
    y -= 28
    steps = [
        ("Own spot desk first", "Paper soft / D0; no futures at start. Miners & audit customers acquire HMC without mid-CEX dump dynamics.", b.URL["exchange"], "exchange.hackme.tech"),
        ("Grow miners + B2B Dig/Hunt demand", "Deepen product usage before external venue pressure.", None, None),
        ("External PoW-friendly CEX later", "When market-making + traction exist.", None, None),
        ("SUP companion, then HMS", "Companion ticker after HMC traction; HMS when storage lane is live.", None, None),
        ("Tier-1 only after legal + volume", "Entity / counsel in progress. Cold mid-tier pitches deferred.", b.URL["legal"], "hackme.tech/legal-risk.html"),
    ]
    for i, (t, d, u, lab) in enumerate(steps, 1):
        y = b.numbered_step(c, b.MARGIN, y, i, t, d, b.W - 2 * b.MARGIN, link_url=u, link_label=lab)
        y -= 2
    c.showPage()


def _asset(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "asset", "03 · Primary asset HMC")
    b.header_bar(c, 3, total, "ASSET")
    b.footer_bar(c)

    b.section_label(c, b.MARGIN, b.H - 58, "02  ·  Primary listing asset")
    b.h1(c, b.MARGIN, b.H - 88, "HackMe Coin — HMC", 20)

    chips = [
        ("Ticker", "HMC", "Native ledger"),
        ("Supply", "No hard cap", "v2 since #550k"),
        ("Decimals", "8", "1 HMC = 1e8 units"),
        ("Block time", "~30s", "Blake3 PoH"),
    ]
    cw = (b.W - 2 * b.MARGIN - 18) / 4
    for i, (lab, val, sub) in enumerate(chips):
        b.metric_chip(c, b.MARGIN + i * (cw + 6), b.H - 180, cw, 58, lab, val, sub)

    y = b.H - 210
    b.section_label(c, b.MARGIN, y, "Chain parameters")
    y -= 18
    rows = [
        ("Field", "Value"),
        ("Name", "HackMe Coin"),
        ("Consensus", "Blake3 PoH + WASM gate (Dig/Hunt)"),
        ("Address format", "HMC- + 64 hex (legacy 16 hex OK)"),
        ("Chain id", "hackme-mainnet-1"),
        ("Signature", "Ed25519 transfer_v1"),
        ("Explorer", "hackme.tech/explorer-lite.html"),
        ("Node API", "GET /api/status"),
        ("Pool PoH stats", "GET …/work/stats"),
        ("Dig/Hunt capacity", "GET …/fuzz/pool/stats"),
        ("Public coin page", "hackme.tech/coin-hmc.html"),
    ]
    links = {
        6: b.URL["explorer"],
        7: b.URL["status"],
        8: b.URL["pool_stats"],
        9: b.URL["fuzz_stat"],
        10: b.URL["coin"],
    }
    y = b.table(c, b.MARGIN, y, [150, b.W - 2 * b.MARGIN - 150], rows, links=links)

    y -= 14
    b.card(c, b.MARGIN, y - 70, b.W - 2 * b.MARGIN, 70, radius=8)
    c.setFillColorRGB(*b.rgb(b.CYAN))
    c.setFont("MonoB", 7.5)
    c.drawString(b.MARGIN + 16, y - 16, "GENESIS TREASURY — DISCLOSED")
    c.setFillColorRGB(*b.rgb(b.TEXT))
    c.setFont("SansB", 11)
    c.drawString(b.MARGIN + 16, y - 36, "50,000 HMC  →")
    aw = c.stringWidth("50,000 HMC  →  ", "SansB", 11)
    b.draw_link(c, "HMC-719006d93916ad52", b.URL["treasury"], b.MARGIN + 16 + aw, y - 36, font="MonoM", size=10)
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Sans", 8.5)
    c.drawString(b.MARGIN + 16, y - 54, "One-time genesis mint · Remaining supply = miner / order emission · click → explorer")

    y -= 96
    b.section_label(c, b.MARGIN, y, "Companion — SUP")
    y -= 18
    rows = [
        ("Field", "Value"),
        ("Ticker", "SUP"),
        ("Max supply", "21,000 SUP (v2 live)"),
        ("Emit", "0.002 SUP / empty block"),
        ("On-chain", "Live — GET /api/sup/economics"),
        ("Listing", "Companion after HMC desk / CEX traction"),
    ]
    b.table(c, b.MARGIN, y, [140, b.W - 2 * b.MARGIN - 140], rows, links={4: b.URL["sup_econ"]})
    c.showPage()


def _diff(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "edge", "04 · Differentiation")
    b.header_bar(c, 4, total, "EDGE")
    b.footer_bar(c)

    b.section_label(c, b.MARGIN, b.H - 58, "03  ·  Differentiation")
    b.h1(c, b.MARGIN, b.H - 88, "Vs typical PoW pools", 20)

    pairs = [
        ("Stratum TCP shares", "HTTP coordinator + hybrid Ed25519"),
        ("Blind hash grinding", "Blake3 share-PPS + Dig/Hunt escrow"),
        ("Single revenue = block subsidy", "Block subsidy + B2B audit escrow"),
        ("Opaque operator wallet", "Public treasury + settlement timers"),
        ("GH/s scoreboard only", "GH/s steers Dig; ASAN stays on CPU"),
    ]
    y = b.H - 120
    mid = b.W / 2
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("MonoB", 7)
    c.drawString(b.MARGIN + 14, y, "TYPICAL POOL")
    c.setFillColorRGB(*b.rgb(b.CYAN))
    c.drawString(mid + 14, y, "HACKME")
    y -= 14
    half = (b.W - 2 * b.MARGIN - 12) / 2
    for left, right in pairs:
        b.card(c, b.MARGIN, y - 48, half, 52, radius=8)
        b.card(c, mid + 6, y - 48, half, 52, radius=8)
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 8.5)
        c.drawString(b.MARGIN + 14, y - 22, b.fit_text(c, left, "Sans", 8.5, half - 28))
        c.setFillColorRGB(*b.rgb(b.TEXT))
        c.setFont("SansB", 8.5)
        words = right.split()
        line1, line2 = "", ""
        for w in words:
            t = (line1 + " " + w).strip()
            if c.stringWidth(t, "SansB", 8.5) < half - 28:
                line1 = t
            else:
                line2 = (line2 + " " + w).strip()
        c.drawString(mid + 20, y - 18, line1)
        if line2:
            c.drawString(mid + 20, y - 32, b.fit_text(c, line2, "SansB", 8.5, half - 40))
        c.setFillColorRGB(*b.rgb(b.CYAN_DIM))
        c.setFont("MonoB", 10)
        c.drawCentredString(mid, y - 22, "→")
        y -= 62

    y -= 8
    b.section_label(c, b.MARGIN, y, "Technology transparency")
    y -= 20
    bullets = [
        ("Open source node · coordinator · workers", b.URL["release"], b.VERSION),
        ("Security research: Hunt Watch 12/12 · OSS CVE Watch (nghttp2 / libheif CLEAN)", None, None),
        ("Marketplace fleet ETA / capacity", b.URL["marketplace"], "fuzz-marketplace.html"),
        ("Economics v2 RFC + Go regression locks", b.URL["rfc_v2"], "RFC_ECONOMICS_V2.md"),
    ]
    for text, url, lab in bullets:
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.circle(b.MARGIN + 5, y + 3, 2.5, fill=1, stroke=0)
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 9)
        max_w = b.W - 2 * b.MARGIN - 20
        if url and lab:
            prefix = text + "  ·  "
            c.drawString(b.MARGIN + 16, y, b.fit_text(c, prefix, "Sans", 9, max_w - 90))
            pw = min(c.stringWidth(prefix, "Sans", 9), max_w - 90)
            b.draw_link(c, lab, url, b.MARGIN + 16 + pw, y, font="MonoM", size=8, max_w=max_w - pw)
        else:
            c.drawString(b.MARGIN + 16, y, b.fit_text(c, text, "Sans", 9, max_w))
        y -= 16

    y -= 6
    b.section_label(c, b.MARGIN, y, "Integration pack")
    y -= 18
    pack = [
        ("1. EXCHANGE_LISTING_WALLET_PREP.md", b.URL["wallet_prep"]),
        ("2. spec/CHAIN_SPEC.md", b.URL["chain_spec"]),
        ("3. docs/API.md — transfers section", b.URL["api_docs"]),
        ("4. Deposit test: transfer_v1 + explorer confirmation", b.URL["explorer"]),
    ]
    for label, url in pack:
        b.draw_link(c, label, url, b.MARGIN, y, font="MonoM", size=8.5, max_w=b.W - 2 * b.MARGIN)
        y -= 16
    c.showPage()


def _tokenomics(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "tokenomics", "05 · Tokenomics")
    b.header_bar(c, 5, total, "TOKENOMICS")
    b.footer_bar(c)

    b.section_label(c, b.MARGIN, b.H - 58, "04  ·  HMC tokenomics")
    b.h1(c, b.MARGIN, b.H - 88, "Supply & emission — v2 live", 20)

    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Sans", 8.5)
    prefix = f"Live since block {b.ACTIVATION} · channel {b.VERSION} · "
    c.drawString(b.MARGIN, b.H - 108, prefix)
    b.draw_link(
        c,
        "GET /api/status",
        b.URL["status"],
        b.MARGIN + c.stringWidth(prefix, "Sans", 8.5),
        b.H - 108,
        font="MonoM",
        size=8,
    )

    y = b.H - 130
    rows = [
        ("Parameter", "Value"),
        ("Max supply", "None (no hard cap)"),
        ("Genesis mint", "50,000 HMC at block #0"),
        ("Genesis recipient", "HMC-719006d93916ad52 (DevFee)"),
        ("Circulating", "total_minted − burned"),
        ("Unit scale", "1 HMC = 1e8 units"),
        ("Address / chain_id", "HMC-+64 hex · hackme-mainnet-1"),
    ]
    y = b.table(c, b.MARGIN, y, [170, b.W - 2 * b.MARGIN - 170], rows, links={3: b.URL["treasury"]})

    y -= 14
    b.card(c, b.MARGIN, y - 44, b.W - 2 * b.MARGIN, 44, radius=8)
    b.body(
        c,
        "No separate pre-mine beyond disclosed genesis treasury. Remaining supply enters via PoH block rewards and order escrow (prepaid by customers, not inflation).",
        b.MARGIN + 14,
        y - 14,
        b.W - 2 * b.MARGIN - 28,
        size=8.5,
        leading=11,
        color=b.SOFT,
    )

    y -= 70
    b.section_label(c, b.MARGIN, y, "Emission schedule — base PoH blocks (v2)")
    y -= 18
    chips = [
        ("Initial base", "0.1 HMC", "per empty block"),
        ("Halving", "4,204,800", "blocks ≈ 4 years"),
        ("Tail floor", "0.002 HMC", "permanent"),
    ]
    cw = (b.W - 2 * b.MARGIN - 12) / 3
    for i, (lab, val, sub) in enumerate(chips):
        b.metric_chip(c, b.MARGIN + i * (cw + 6), y - 58, cw, 58, lab, val, sub)

    y -= 80
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Mono", 7.5)
    c.drawString(b.MARGIN, y, "reward_base(height) = max(0.002, 0.1 / 2^epoch)")
    y -= 12
    c.drawString(b.MARGIN, y, "epoch = floor((height-1) / 4,204,800)  ·  first v2 halving @ #4,204,801")

    y -= 28
    b.section_label(c, b.MARGIN, y, "Fee & burn mechanics (v2)")
    y -= 18
    rows = [
        ("Flow", "Rate / destination"),
        ("Transfer fee split", "70% DevFee · 30% burn tally"),
        ("Order platform fee", "5% of prepaid → DevFee"),
        ("Order burn", "5% real debit → 2.5% → 1.25%"),
        ("Fee asset", "HMC or SUP (escrow always HMC)"),
    ]
    y = b.table(c, b.MARGIN, y, [170, b.W - 2 * b.MARGIN - 170], rows)

    y -= 18
    b.section_label(c, b.MARGIN, y, "Why HMC is needed")
    y -= 16
    utils = [
        "Mining settlement — coordinator accrual settles to on-chain HMC",
        "B2B security audits — customers escrow HMC for WASM fuzz / phasing",
        "Network fees — transfers and order creation debit HMC",
        "Ecosystem rail — SUP accrual tied to honest HMC pool work; HMS prepaid in HMC",
    ]
    for u in utils:
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.circle(b.MARGIN + 5, y + 3, 2.5, fill=1, stroke=0)
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 8.5)
        c.drawString(b.MARGIN + 16, y, b.fit_text(c, u, "Sans", 8.5, b.W - 2 * b.MARGIN - 20))
        y -= 15
    c.showPage()


def _allocation(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "allocation", "06 · Allocation & vesting")
    b.header_bar(c, 6, total, "ALLOCATION")
    b.footer_bar(c)

    b.section_label(c, b.MARGIN, b.H - 58, "05  ·  Allocation & vesting")
    b.h1(c, b.MARGIN, b.H - 88, "Cap table disclosure", 20)
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Sans", 8.5)
    sub = "Listing-grade disclosure · HMC / SUP / HMS. Live balances → "
    c.drawString(b.MARGIN, b.H - 106, sub)
    b.draw_link(
        c,
        "token-transparency.html",
        b.URL["transparency"],
        b.MARGIN + c.stringWidth(sub, "Sans", 8.5),
        b.H - 106,
        font="MonoM",
        size=8,
        max_w=b.W - b.MARGIN - b.MARGIN - c.stringWidth(sub, "Sans", 8.5),
    )

    y = b.H - 128
    b.section_label(c, b.MARGIN, y, "HMC · no hard cap (v2)")
    y -= 16
    rows = [
        ("Category", "% / amount / vesting"),
        ("Genesis treasury (DevFee)", "50,000 · unlocked at genesis"),
        ("Miner + order emission", "Open-ended · continuous PoH + escrow"),
        ("Team pre-mine", "0% · none declared"),
        ("Investor SAFT", "0% · none declared"),
    ]
    y = b.table(c, b.MARGIN, y, [200, b.W - 2 * b.MARGIN - 200], rows)

    y -= 12
    b.card(c, b.MARGIN, y - 56, b.W - 2 * b.MARGIN, 56, radius=8)
    c.setFillColorRGB(*b.rgb(b.CYAN))
    c.setFont("MonoB", 7)
    c.drawString(b.MARGIN + 14, y - 14, "PUBLIC TREASURY")
    b.draw_link(c, "HMC-719006d93916ad52", b.URL["treasury"], b.MARGIN + 14, y - 32, font="MonoM", size=9)
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Sans", 8)
    c.drawString(b.MARGIN + 14, y - 48, "Explorer-verifiable · no hidden team cliff · top-10 holder report quarterly")

    y -= 80
    half = (b.W - 2 * b.MARGIN - 12) / 2
    b.card(c, b.MARGIN, y - 150, half, 150, radius=10)
    b.card(c, b.MARGIN + half + 12, y - 150, half, 150, radius=10)

    c.setFillColorRGB(*b.rgb(b.CYAN))
    c.setFont("MonoB", 7.5)
    c.drawString(b.MARGIN + 16, y - 18, "SUP — MAX 21,000")
    c.setFillColorRGB(*b.rgb(b.TEXT))
    c.setFont("SansB", 11)
    c.drawString(b.MARGIN + 16, y - 40, "Block emit 0.002 SUP")
    c.setFillColorRGB(*b.rgb(b.SOFT))
    c.setFont("Sans", 8)
    for i, line in enumerate(
        ["0% public / team pre-mine", "Fee asset only (not Dig escrow)", "No vesting cliffs", "Cap includes pre-v2 mint"]
    ):
        c.drawString(b.MARGIN + 16, y - 62 - i * 14, "·  " + line)

    c.setFillColorRGB(*b.rgb(b.GOLD))
    c.setFont("MonoB", 7.5)
    c.drawString(b.MARGIN + half + 28, y - 18, "HMS — PRELAUNCH")
    c.setFillColorRGB(*b.rgb(b.TEXT))
    c.setFont("SansB", 11)
    c.drawString(b.MARGIN + half + 28, y - 40, "Max 21,000,000 HMS")
    c.setFillColorRGB(*b.rgb(b.SOFT))
    c.setFont("Sans", 8)
    for i, line in enumerate(
        [
            "Genesis float 0.5% · 105,000",
            "Storage + seal emission 99.5%",
            "Lane not live on hub yet",
            "Do not trade until genesis announced",
        ]
    ):
        c.drawString(b.MARGIN + half + 28, y - 62 - i * 14, "·  " + line)

    y -= 178
    b.section_label(c, b.MARGIN, y, "Cross-ecosystem principles")
    y -= 16
    principles = [
        "Separate ledgers — HMC, SUP, HMS do not share emission curves",
        "Work-based SUP — no click-to-claim",
        "Customer-funded orders — B2B HMC escrow is demand, not mint-and-dump",
        "Public treasury — genesis addresses published; no opaque multi-sig without disclosure",
    ]
    for p in principles:
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.circle(b.MARGIN + 5, y + 3, 2.5, fill=1, stroke=0)
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 8.5)
        c.drawString(b.MARGIN + 16, y, b.fit_text(c, p, "Sans", 8.5, b.W - 2 * b.MARGIN - 20))
        y -= 15
    c.showPage()


def _dd(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "dd", "07 · Due diligence")
    b.header_bar(c, 7, total, "DUE DILIGENCE")
    b.footer_bar(c)

    b.section_label(c, b.MARGIN, b.H - 58, "06  ·  Red-flag checklist")
    b.h1(c, b.MARGIN, b.H - 88, "Binance-style DD answers", 20)

    y = b.H - 118
    qa = [
        ("Large team allocation without lockup?", "No declared team pre-mine on HMC / SUP", None),
        ("Opaque treasury?", "DevFee address public; explorer balances", b.URL["treasury"]),
        ("Circulating supply unknown?", "API + transparency page", b.URL["transparency"]),
        ("Weak utility?", "Mining + audits + fees; SUP/HMS extend utility", b.URL["listing"]),
        ("Unverified inflation?", "Policy hash in /api/status; Go regression locks", b.URL["status"]),
    ]
    for q, a, url in qa:
        b.card(c, b.MARGIN, y - 52, b.W - 2 * b.MARGIN, 52, radius=8)
        c.setFillColorRGB(*b.rgb(b.MUTED))
        c.setFont("Mono", 7)
        c.drawString(b.MARGIN + 16, y - 14, "QUESTION")
        c.setFillColorRGB(*b.rgb(b.TEXT))
        c.setFont("SansB", 9.5)
        c.drawString(b.MARGIN + 16, y - 30, b.fit_text(c, q, "SansB", 9.5, b.W - 2 * b.MARGIN - 32))
        if url:
            b.draw_link(c, a, url, b.MARGIN + 16, y - 44, font="Sans", size=8.5, max_w=b.W - 2 * b.MARGIN - 32)
        else:
            c.setFillColorRGB(*b.rgb(b.CYAN))
            c.setFont("Sans", 8.5)
            c.drawString(b.MARGIN + 16, y - 44, a)
        y -= 60

    y -= 4
    b.section_label(c, b.MARGIN, y, "Market / liquidity — honest")
    y -= 16
    bullets = [
        "Stage: early PoW network · official pool live · own spot desk paper soft",
        "Near-term: desk UX + miners + audit customers — not paid mid-CEX listing",
        "Later CEX: PoW-friendly venues when MM budget exists",
        "Liquidity: no external MM engaged yet · no ROI promises in official channels",
    ]
    for bl in bullets:
        c.setFillColorRGB(*b.rgb(b.CYAN))
        c.circle(b.MARGIN + 5, y + 3, 2.5, fill=1, stroke=0)
        c.setFillColorRGB(*b.rgb(b.SOFT))
        c.setFont("Sans", 8.5)
        c.drawString(b.MARGIN + 16, y, b.fit_text(c, bl, "Sans", 8.5, b.W - 2 * b.MARGIN - 20))
        y -= 14

    y -= 10
    b.section_label(c, b.MARGIN, y, "Legal")
    y -= 16
    c.setFillColorRGB(*b.rgb(b.SOFT))
    c.setFont("Sans", 8.5)
    c.drawString(b.MARGIN, y, "·  Risk disclosures:")
    b.draw_link(c, "hackme.tech/legal-risk.html", b.URL["legal"], b.MARGIN + 108, y, font="MonoM", size=8)
    y -= 14
    c.setFillColorRGB(*b.rgb(b.SOFT))
    c.setFont("Sans", 8.5)
    c.drawString(b.MARGIN, y, "·  Not registered as a security offering — utility + mining infrastructure narrative")
    y -= 14
    c.drawString(b.MARGIN, y, "·  Entity / counsel: in progress for Tier-1 exchanges")
    c.showPage()


def _links(c: canvas.Canvas, logo: str, total: int):
    b.fill_bg(c)
    b.bookmark(c, "links", "08 · Links & verification")
    b.header_bar(c, 8, total, "LINKS")
    b.footer_bar(c)

    b.section_label(c, b.MARGIN, b.H - 58, "07  ·  Social proof & links")
    b.h1(c, b.MARGIN, b.H - 88, "Where to verify", 20)

    links = [
        ("Website", "https://hackme.tech", b.URL["home"]),
        ("Listing hub", "https://hackme.tech/listing.html", b.URL["listing"]),
        ("Contacts", "https://hackme.tech/contacts.html", b.URL["contacts"]),
        ("Explorer", "https://hackme.tech/explorer-lite.html", b.URL["explorer"]),
        ("Spot desk", "https://exchange.hackme.tech/", b.URL["exchange"]),
        ("GitHub", "https://github.com/jokeez/hackme", b.URL["github"]),
        ("Telegram", "t.me/hackme_tech", b.URL["telegram"]),
        ("Bitcointalk", "bitcointalk.org · topic 5583373", b.URL["bitcointalk"]),
        ("Token transparency", "hackme.tech/token-transparency.html", b.URL["transparency"]),
        ("Release", f"{b.VERSION} on GitHub", b.URL["release"]),
    ]

    y = b.H - 120
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
            display = b.fit_text(c, val, "MonoM", 8, card_w - 28)
            c.setFillColorRGB(*b.rgb(b.CYAN))
            c.setFont("MonoM", 8)
            c.drawString(x + 14, y - 32, display)
            c.setStrokeColorRGB(*b.rgb(b.CYAN_DIM))
            c.setLineWidth(0.5)
            c.line(x + 14, y - 34, x + 14 + c.stringWidth(display, "MonoM", 8), y - 34)
        y -= 58

    y -= 8
    b.card(c, b.MARGIN, y - 92, b.W - 2 * b.MARGIN, 92, radius=12)
    b.link_box(c, b.URL["listing"], b.MARGIN, y - 92, b.W - 2 * b.MARGIN, 92)
    c.drawImage(logo, b.MARGIN + 18, y - 80, width=56, height=56, mask="auto")
    c.setFillColorRGB(*b.rgb(b.TEXT))
    c.setFont("SansB", 15)
    c.drawString(b.MARGIN + 92, y - 34, "Ready for exchange diligence")
    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Sans", 8.5)
    c.drawString(b.MARGIN + 92, y - 52, "Full memo · tokenomics v2 · allocation · red-flag answers")
    b.draw_link(c, "hackme.tech/listing.html", b.URL["listing"], b.MARGIN + 92, y - 72, font="MonoM", size=9)

    c.setFillColorRGB(*b.rgb(b.MUTED))
    c.setFont("Mono", 7)
    c.drawCentredString(b.W / 2, 48, f"END OF PACK  ·  HackMe Network  ·  {b.PACK_DATE_LONG}")
    c.showPage()
