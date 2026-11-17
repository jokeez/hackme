"""Shared HackMe listing-PDF brand kit (coal + cyan + soft gold)."""

from __future__ import annotations

from pathlib import Path

from PIL import Image as PILImage
from reportlab.lib.pagesizes import letter
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.pdfgen import canvas

ROOT = Path(__file__).resolve().parents[3]
PACK_DIR = Path(__file__).resolve().parent
ASSETS = PACK_DIR / "assets"
LOGO_SRC = ROOT / "web" / "site" / "assets" / "logo-hex.png"
OUT_DIR = ROOT / "dist" / "docs"

VERSION = "0.1.0-rc19"
PACK_DATE = "2026.11.17"
PACK_DATE_LONG = "November 17, 2026"
ACTIVATION = "550,000"

BG = (7, 11, 16)
SURFACE = (12, 18, 26)
CARD = (18, 28, 40)
CARD2 = (24, 36, 50)
LINE = (42, 58, 78)
CYAN = (45, 230, 220)
CYAN_DIM = (28, 140, 135)
GOLD = (220, 180, 70)
TEXT = (232, 238, 245)
MUTED = (130, 148, 168)
SOFT = (180, 196, 214)

W, H = letter
MARGIN = 42

URL = {
    "home": "https://hackme.tech",
    "listing": "https://hackme.tech/listing.html",
    "contacts": "https://hackme.tech/contacts.html",
    "explorer": "https://hackme.tech/explorer-lite.html",
    "status": "https://hackme.tech/api/status",
    "sup_econ": "https://hackme.tech/api/sup/economics",
    "pool_stats": "https://hackme.tech/pool/coordinator/api/work/stats",
    "fuzz_stat": "https://hackme.tech/pool/coordinator/api/fuzz/pool/stats",
    "marketplace": "https://hackme.tech/fuzz-marketplace.html",
    "legal": "https://hackme.tech/legal-risk.html",
    "legal_hub": "https://hackme.tech/legal.html",
    "privacy": "https://hackme.tech/legal-privacy.html",
    "transparency": "https://hackme.tech/token-transparency.html",
    "coin": "https://hackme.tech/coin-hmc.html",
    "coin_sup": "https://hackme.tech/coin-sup.html",
    "coin_hms": "https://hackme.tech/coin-hms.html",
    "economics": "https://hackme.tech/economics-model.html",
    "downloads": "https://hackme.tech/downloads.html",
    "exchange": "https://exchange.hackme.tech/",
    "github": "https://github.com/jokeez/hackme",
    "release": f"https://github.com/jokeez/hackme/releases/tag/{VERSION}",
    "telegram": "https://t.me/hackme_tech",
    "bitcointalk": "https://bitcointalk.org/index.php?topic=5583373",
    "treasury": "https://hackme.tech/explorer-lite.html#HMC-719006d93916ad52",
    "wallet_prep": "https://github.com/jokeez/hackme/blob/main/docs/internal/EXCHANGE_LISTING_WALLET_PREP.md",
    "chain_spec": "https://github.com/jokeez/hackme/blob/main/spec/CHAIN_SPEC.md",
    "api_docs": "https://github.com/jokeez/hackme/blob/main/docs/API.md",
    "rfc_v2": "https://github.com/jokeez/hackme/blob/main/docs/RFC_ECONOMICS_V2.md",
    "license": "https://github.com/jokeez/hackme/blob/main/LICENSE",
}


def rgb(c):
    return tuple(x / 255 for x in c)


def reg_fonts():
    pdfmetrics.registerFont(TTFont("Sans", "/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf"))
    pdfmetrics.registerFont(TTFont("SansB", "/usr/share/fonts/truetype/liberation/LiberationSans-Bold.ttf"))
    pdfmetrics.registerFont(TTFont("Mono", "/usr/share/fonts/truetype/jetbrains-mono/JetBrainsMono-Regular.ttf"))
    pdfmetrics.registerFont(TTFont("MonoB", "/usr/share/fonts/truetype/jetbrains-mono/JetBrainsMono-Bold.ttf"))
    pdfmetrics.registerFont(TTFont("MonoM", "/usr/share/fonts/truetype/jetbrains-mono/JetBrainsMono-Medium.ttf"))


def prepare_logo():
    ASSETS.mkdir(parents=True, exist_ok=True)
    out = ASSETS / "logo-hex-dark.png"
    im = PILImage.open(LOGO_SRC).convert("RGBA")
    bg = PILImage.new("RGBA", im.size, (*BG, 255))
    composed = PILImage.alpha_composite(bg, im)
    composed.convert("RGB").save(out, "PNG")
    glow = ASSETS / "logo-plate.png"
    composed.resize((420, 420), PILImage.Resampling.LANCZOS).save(glow, "PNG")
    return str(out), str(glow)


def fill_bg(c: canvas.Canvas):
    c.setFillColorRGB(*rgb(BG))
    c.rect(0, 0, W, H, fill=1, stroke=0)
    c.saveState()
    for i in range(90):
        a = 0.035 * (1 - i / 90)
        c.setFillColorRGB(CYAN[0] / 255, CYAN[1] / 255, CYAN[2] / 255, alpha=a)
        c.rect(0, H - i * 2.2, W, 2.2, fill=1, stroke=0)
    for i in range(40):
        a = 0.02 * (1 - i / 40)
        c.setFillColorRGB(GOLD[0] / 255, GOLD[1] / 255, GOLD[2] / 255, alpha=a)
        c.rect(0, i * 1.4, W, 1.4, fill=1, stroke=0)
    c.restoreState()
    c.setFillAlpha(1)
    c.setStrokeAlpha(1)


def draw_hex_watermark(c: canvas.Canvas, logo_path: str, x, y, size=220, alpha=0.08):
    c.saveState()
    c.setFillAlpha(alpha)
    c.setStrokeAlpha(alpha)
    c.drawImage(logo_path, x - size / 2, y - size / 2, width=size, height=size, mask="auto", preserveAspectRatio=True)
    c.restoreState()
    c.setFillAlpha(1)
    c.setStrokeAlpha(1)


def link_box(c: canvas.Canvas, url: str, x, y, w, h):
    if not url:
        return
    c.linkURL(url, (x, y, x + w, y + h), relative=0, thickness=0)


def fit_text(c: canvas.Canvas, text: str, font: str, size: float, max_w: float) -> str:
    if c.stringWidth(text, font, size) <= max_w:
        return text
    while text and c.stringWidth(text + "…", font, size) > max_w:
        text = text[:-1]
    return text + "…" if text else ""


def draw_link(
    c: canvas.Canvas,
    text: str,
    url: str,
    x: float,
    y: float,
    font: str = "MonoM",
    size: float = 9,
    color=CYAN,
    underline: bool = True,
    max_w: float | None = None,
):
    display = fit_text(c, text, font, size, max_w) if max_w else text
    c.setFillColorRGB(*rgb(color))
    c.setFont(font, size)
    c.drawString(x, y, display)
    tw = c.stringWidth(display, font, size)
    if underline:
        c.setStrokeColorRGB(*rgb(CYAN_DIM))
        c.setLineWidth(0.6)
        c.line(x, y - 2, x + tw, y - 2)
    link_box(c, url, x - 2, y - 3, tw + 4, size + 6)
    return tw


def header_bar(c: canvas.Canvas, page: int, total: int, title: str = "LISTING PACK"):
    c.setStrokeColorRGB(*rgb(LINE))
    c.setLineWidth(0.6)
    c.line(MARGIN, H - 28, W - MARGIN, H - 28)
    label = "HACKME NETWORK"
    c.setFillColorRGB(*rgb(CYAN))
    c.setFont("MonoM", 7)
    c.drawString(MARGIN, H - 22, label)
    tw = c.stringWidth(label, "MonoM", 7)
    link_box(c, URL["home"], MARGIN - 2, H - 28, tw + 4, 14)
    c.setFillColorRGB(*rgb(MUTED))
    c.setFont("Mono", 7)
    c.drawRightString(W - MARGIN, H - 22, f"{title}  ·  {page:02d} / {total:02d}")


def footer_bar(c: canvas.Canvas, right: str = "Confidential for exchange DD"):
    c.setStrokeColorRGB(*rgb(LINE))
    c.setLineWidth(0.6)
    c.line(MARGIN, 28, W - MARGIN, 28)
    draw_link(c, "hackme.tech", URL["home"], MARGIN, 16, font="Mono", size=6.5, underline=True)
    rest = "  ·  AGPL-3.0  ·  Not investment advice"
    c.setFillColorRGB(*rgb(MUTED))
    c.setFont("Mono", 6.5)
    c.drawString(MARGIN + c.stringWidth("hackme.tech", "Mono", 6.5), 16, rest)
    c.drawRightString(W - MARGIN, 16, right)


def section_label(c: canvas.Canvas, x, y, text: str):
    c.setFillColorRGB(*rgb(CYAN))
    c.setFont("MonoB", 7.5)
    label = text.upper()
    c.drawString(x, y, label)
    tw = c.stringWidth(label, "MonoB", 7.5)
    c.setStrokeColorRGB(*rgb(CYAN_DIM))
    c.setLineWidth(1.2)
    c.line(x + tw + 8, y + 2, min(x + tw + 36, W - MARGIN), y + 2)


def h1(c: canvas.Canvas, x, y, text: str, size=22):
    c.setFillColorRGB(*rgb(TEXT))
    c.setFont("SansB", size)
    c.drawString(x, y, text)


def body(c: canvas.Canvas, text: str, x, y, max_w, size=9, leading=13, color=SOFT):
    c.setFillColorRGB(*rgb(color))
    c.setFont("Sans", size)
    words = text.split()
    line = ""
    yy = y
    for w in words:
        test = (line + " " + w).strip()
        if c.stringWidth(test, "Sans", size) <= max_w:
            line = test
        else:
            if line:
                c.drawString(x, yy, line)
                yy -= leading
            # hard-break overly long tokens
            while c.stringWidth(w, "Sans", size) > max_w:
                cut = len(w)
                while cut > 1 and c.stringWidth(w[:cut], "Sans", size) > max_w:
                    cut -= 1
                c.drawString(x, yy, w[:cut])
                yy -= leading
                w = w[cut:]
            line = w
    if line:
        c.drawString(x, yy, line)
        yy -= leading
    return yy


def card(c: canvas.Canvas, x, y, w, h, radius=10):
    c.setFillColorRGB(*rgb(CARD))
    c.roundRect(x, y, w, h, radius, fill=1, stroke=0)
    c.setStrokeColorRGB(*rgb(LINE))
    c.setLineWidth(0.8)
    c.roundRect(x, y, w, h, radius, fill=0, stroke=1)


def metric_chip(c: canvas.Canvas, x, y, w, h, label, value, sub=None):
    card(c, x, y, w, h, radius=8)
    c.setFillColorRGB(*rgb(CYAN))
    c.rect(x, y + 6, 3, h - 12, fill=1, stroke=0)
    c.setFillColorRGB(*rgb(MUTED))
    c.setFont("Mono", 6.5)
    c.drawString(x + 14, y + h - 16, label.upper())
    c.setFillColorRGB(*rgb(TEXT))
    val_size = 12 if len(value) > 14 else 13
    c.setFont("SansB", val_size)
    c.drawString(x + 14, y + h - 34, fit_text(c, value, "SansB", val_size, w - 28))
    if sub:
        c.setFillColorRGB(*rgb(MUTED))
        c.setFont("Sans", 7.5)
        c.drawString(x + 14, y + 12, fit_text(c, sub, "Sans", 7.5, w - 28))


def table(c: canvas.Canvas, x, y, col_w, rows, header=True, links=None):
    links = links or {}
    row_h = 22
    yy = y
    for i, (k, v) in enumerate(rows):
        display = v
        if header and i == 0:
            c.setFillColorRGB(*rgb(CARD2))
            c.roundRect(x, yy - 4, sum(col_w), row_h + 2, 4, fill=1, stroke=0)
            c.setFillColorRGB(*rgb(CYAN))
            c.setFont("MonoB", 7)
            c.drawString(x + 10, yy + 4, k)
            c.drawString(x + col_w[0] + 8, yy + 4, v)
        else:
            if i % 2 == (1 if header else 0):
                c.setFillColorRGB(*rgb(SURFACE))
                c.rect(x, yy - 4, sum(col_w), row_h, fill=1, stroke=0)
            c.setFillColorRGB(*rgb(MUTED))
            c.setFont("Sans", 8)
            c.drawString(x + 10, yy + 4, fit_text(c, k, "Sans", 8, col_w[0] - 16))
            font, size = "MonoM", 7.5
            max_vw = col_w[1] - 16
            display = fit_text(c, display, font, size, max_vw)
            url = links.get(i)
            if url:
                draw_link(c, display, url, x + col_w[0] + 8, yy + 4, font=font, size=size)
            else:
                c.setFillColorRGB(*rgb(TEXT))
                c.setFont(font, size)
                c.drawString(x + col_w[0] + 8, yy + 4, display)
        yy -= row_h
    return yy


def numbered_step(c: canvas.Canvas, x, y, n, title, desc, w, link_url=None, link_label=None):
    c.setFillColorRGB(*rgb(CARD2))
    c.circle(x + 12, y + 4, 12, fill=1, stroke=0)
    c.setStrokeColorRGB(*rgb(CYAN_DIM))
    c.setLineWidth(1)
    c.circle(x + 12, y + 4, 12, fill=0, stroke=1)
    c.setFillColorRGB(*rgb(CYAN))
    c.setFont("MonoB", 9)
    c.drawCentredString(x + 12, y + 1, str(n))
    c.setFillColorRGB(*rgb(TEXT))
    c.setFont("SansB", 9.5)
    c.drawString(x + 34, y + 6, fit_text(c, title, "SansB", 9.5, w - 40))
    yy = body(c, desc, x + 34, y - 8, w - 34, size=8, leading=11, color=MUTED)
    if link_url and link_label:
        draw_link(c, link_label, link_url, x + 34, yy + 2, font="MonoM", size=7.5, max_w=w - 40)
        yy -= 10
    return yy - 6


def bookmark(c: canvas.Canvas, key: str, title: str, level: int = 0):
    c.bookmarkPage(key)
    c.addOutlineEntry(title, key, level=level, closed=0)


def cover_chrome(c: canvas.Canvas, logo: str, glow: str, kicker: str, title_left: str, title_right: str, subtitle: str):
    fill_bg(c)
    c.saveState()
    c.setFillAlpha(0.12)
    c.drawImage(glow, W / 2 - 210, H / 2 - 20, width=420, height=420, mask="auto")
    c.restoreState()
    c.setFillAlpha(1)

    c.setFillColorRGB(*rgb(CYAN))
    c.setFont("MonoM", 8)
    c.drawString(MARGIN, H - 52, kicker)
    c.setFillColorRGB(*rgb(MUTED))
    c.setFont("Mono", 8)
    c.drawRightString(W - MARGIN, H - 52, f"v{PACK_DATE}")
    c.setStrokeColorRGB(*rgb(CYAN))
    c.setLineWidth(1.5)
    c.line(MARGIN, H - 64, MARGIN + 72, H - 64)

    c.setFillColorRGB(*rgb(CARD))
    c.roundRect(MARGIN - 8, H - 188, 96, 96, 14, fill=1, stroke=0)
    c.setStrokeColorRGB(*rgb(LINE))
    c.setLineWidth(1)
    c.roundRect(MARGIN - 8, H - 188, 96, 96, 14, fill=0, stroke=1)
    c.drawImage(logo, MARGIN, H - 180, width=80, height=80, mask="auto")

    c.setFillColorRGB(*rgb(TEXT))
    c.setFont("SansB", 36)
    c.drawString(MARGIN, H - 236, title_left)
    left_w = c.stringWidth(title_left, "SansB", 36)
    c.setFillColorRGB(*rgb(CYAN))
    c.setFont("SansB", 28)
    c.drawString(MARGIN + left_w + 12, H - 232, title_right)

    c.setFillColorRGB(*rgb(SOFT))
    c.setFont("Sans", 11)
    c.drawString(MARGIN, H - 260, fit_text(c, subtitle, "Sans", 11, W - 2 * MARGIN))


def cover_footer_card(c: canvas.Canvas):
    card(c, MARGIN, 52, W - 2 * MARGIN, 100, radius=12)
    c.setFillColorRGB(*rgb(MUTED))
    c.setFont("Mono", 6.5)
    c.drawString(MARGIN + 20, 132, "PREPARED BY")
    c.setFillColorRGB(*rgb(TEXT))
    c.setFont("SansB", 10)
    c.drawString(MARGIN + 20, 116, "HackMe contributors")
    c.setFillColorRGB(*rgb(MUTED))
    c.setFont("Mono", 6.5)
    c.drawString(MARGIN + 20, 92, "CONTACT")
    draw_link(c, "https://hackme.tech", URL["home"], MARGIN + 20, 76, font="MonoM", size=9)

    c.setFillColorRGB(*rgb(MUTED))
    c.setFont("Mono", 6.5)
    c.drawString(W / 2 + 10, 132, "REPOSITORY")
    draw_link(c, "github.com/jokeez/hackme", URL["github"], W / 2 + 10, 116, font="MonoM", size=8.5)
    c.setFillColorRGB(*rgb(MUTED))
    c.setFont("Mono", 6.5)
    c.drawString(W / 2 + 10, 92, "DISCLAIMER")
    c.setFillColorRGB(*rgb(SOFT))
    c.setFont("Sans", 8)
    c.drawString(W / 2 + 10, 76, "Not investment advice · Utility + mining infra")


def new_doc(path: Path, title: str, subject: str) -> canvas.Canvas:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    c = canvas.Canvas(str(path), pagesize=letter)
    c.setTitle(title)
    c.setAuthor("HackMe contributors")
    c.setSubject(subject)
    c.setCreator("HackMe PDF pack builder")
    return c
