# Documentation export — PDF packs

HackMe maintains **Markdown as source of truth** on GitHub and **HTML on hackme.tech**. Exchange DD gets **branded PDF** packs (ReportLab · dark coal + cyan).

## Quick build

```bash
# From repo root — requires Python 3 + reportlab + Pillow
bash scripts/release/build_listing_pdfs.sh
# first time:
#   python3 -m venv scripts/release/pdf_pack/.venv
#   scripts/release/pdf_pack/.venv/bin/pip install reportlab Pillow
```

Output:

| File | Contents |
|------|----------|
| `dist/docs/HMC_Listing_Pack.pdf` | Cover · memo · asset · edge · tokenomics v2 · allocation · DD · links |
| `dist/docs/SUP_Companion_Overview.pdf` | SUP positioning · supply · utility · listing policy |
| `dist/docs/HackMe_Network_Pitch.pdf` | Institutional pitch (problem → ask) |
| `dist/docs/HackMe_Legal_and_Rights.pdf` | License · trademark · tokens · live snapshot · risk |
| `dist/docs/AGPL-3.0_License.pdf` | Full AGPL-3.0 text (branded) |
| `dist/docs/SHA256SUMS-docs.txt` | SHA256 checksums |

Builder: `scripts/release/pdf_pack/` · truth stamped to Economics v2 (live since block 550,000) · channel from `scripts/release/CURRENT_VERSION`.

Published URLs (after deploy):

- https://hackme.tech/dist/docs/HMC_Listing_Pack.pdf
- https://hackme.tech/dist/docs/SUP_Companion_Overview.pdf
- https://hackme.tech/dist/docs/HackMe_Network_Pitch.pdf
- https://hackme.tech/dist/docs/HackMe_Legal_and_Rights.pdf
- https://hackme.tech/dist/docs/AGPL-3.0_License.pdf

## Source of truth (content)

| Pack | Canonical docs |
|------|----------------|
| HMC | `EXCHANGE_LISTING_MEMO.md` · `HMC_TOKENOMICS.md` · `TOKEN_ALLOCATION_AND_VESTING.md` · `RFC_ECONOMICS_V2.md` |
| SUP | `SUP_TOKENOMICS.md` · `SUPPORT_COIN_UTILITY.md` |
| Pitch | `LISTING_PITCH_OUTLINE.md` · `ECOSYSTEM_OVERVIEW.md` |
| Legal | `RIGHTS_AND_DISCLOSURES.md` · `TRADEMARK_AND_FORKING.md` |
| AGPL | `LICENSE` |

## Deploy PDFs to hackme.tech

```bash
HACKME_DEPLOY_SSH_IDENTITY=~/.ssh/cursor_vps NODE_SSH=hackme-vps \
  SKIP_NODE=1 bash scripts/ops/deploy_hackme_site.sh
```

(`dist/` rsync includes `dist/docs/` when present.)
