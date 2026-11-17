#!/usr/bin/env python3
"""Build all HackMe listing PDFs into dist/docs/."""

from __future__ import annotations

import hashlib
import sys
from pathlib import Path

# Allow `python build_all.py` from this directory
_HERE = Path(__file__).resolve().parent
if str(_HERE.parent) not in sys.path:
    sys.path.insert(0, str(_HERE.parent))

from pdf_pack import agpl, brand, hmc, legal, pitch, sup  # noqa: E402


def main() -> int:
    brand.reg_fonts()
    logo, glow = brand.prepare_logo()
    out = brand.OUT_DIR
    out.mkdir(parents=True, exist_ok=True)

    built = [
        hmc.build(logo, glow, out / "HMC_Listing_Pack.pdf"),
        sup.build(logo, glow, out / "SUP_Companion_Overview.pdf"),
        pitch.build(logo, glow, out / "HackMe_Network_Pitch.pdf"),
        legal.build(logo, glow, out / "HackMe_Legal_and_Rights.pdf"),
        agpl.build(logo, glow, out / "AGPL-3.0_License.pdf"),
    ]

    sums = out / "SHA256SUMS-docs.txt"
    with sums.open("w", encoding="utf-8") as f:
        for p in built:
            digest = hashlib.sha256(p.read_bytes()).hexdigest()
            f.write(f"{digest}  {p.name}\n")
            print(f"[pdf] {p.name}  ({p.stat().st_size} bytes)  {digest[:16]}…")
    print(f"[pdf] wrote {sums}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
