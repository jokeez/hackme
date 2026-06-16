# Documentation export — PDF / DOCX per ticker

HackMe maintains **Markdown as source of truth** on GitHub and **HTML on hackme.tech**. Exchange DD and investors often need **PDF or DOCX** attachments.

## Quick build (recommended)

```bash
# From repo root — requires pandoc + texlive-xetex + fonts-dejavu-core
bash scripts/release/build_listing_pdfs.sh
```

Output:

| File | Contents |
|------|----------|
| `dist/docs/HMC_Listing_Pack.pdf` | Listing memo + HMC tokenomics + allocation |
| `dist/docs/SUP_Companion_Overview.pdf` | SUP tokenomics + utility policy |
| `dist/docs/HackMe_Network_Pitch.pdf` | Pitch outline + ecosystem overview |
| `dist/docs/HackMe_Legal_and_Rights.pdf` | Rights, disclosures, trademark & forking |
| `dist/docs/AGPL-3.0_License.pdf` | Full AGPL-3.0 license text (verbatim) |
| `dist/docs/SHA256SUMS-docs.txt` | SHA256 checksums |

Branding: `scripts/release/pandoc/hackme-header.tex` (title page, headers, HackMe colors).

Published URLs (after deploy):

- https://hackme.tech/dist/docs/HMC_Listing_Pack.pdf
- https://hackme.tech/dist/docs/SUP_Companion_Overview.pdf
- https://hackme.tech/dist/docs/HackMe_Network_Pitch.pdf
- https://hackme.tech/dist/docs/HackMe_Legal_and_Rights.pdf
- https://hackme.tech/dist/docs/AGPL-3.0_License.pdf

## Manual pandoc (if script unavailable)

```bash
mkdir -p dist/docs

pandoc docs/EXCHANGE_LISTING_MEMO.md docs/HMC_TOKENOMICS.md \
  docs/TOKEN_ALLOCATION_AND_VESTING.md \
  --pdf-engine=xelatex \
  -V mainfont="DejaVu Sans" \
  -V geometry:margin=1in \
  -o dist/docs/HMC_Listing_Pack.pdf

pandoc docs/SUP_TOKENOMICS.md docs/SUPPORT_COIN_UTILITY.md \
  --pdf-engine=xelatex \
  -V mainfont="DejaVu Sans" \
  -o dist/docs/SUP_Companion_Overview.pdf
```

**Common pitfalls:**

| Error | Fix |
|-------|-----|
| `does not exist` on input | Run from repo root (`cd ~/Desktop/HackMe`) |
| `dist/docs/... does not exist` | `mkdir -p dist/docs` first |
| Unicode LaTeX error | Use `--pdf-engine=xelatex` and `-V mainfont="DejaVu Sans"` |
| `xelatex not found` | `sudo apt install texlive-xetex fonts-dejavu-core` |
| Wrong output filename | SUP pack must be `SUP_Companion_Overview.pdf`, not `HMC_Listing_Pack.pdf` |

## Per-ticker document sets

### HMC pack (`HMC_Listing_Pack`)

| Section | Source file |
|---------|-------------|
| Cover + memo | `docs/EXCHANGE_LISTING_MEMO.md` |
| Tokenomics | `docs/HMC_TOKENOMICS.md` |
| Allocation & vesting | `docs/TOKEN_ALLOCATION_AND_VESTING.md` |

### SUP pack (`SUP_Companion_Overview`)

| Section | Source file |
|---------|-------------|
| Tokenomics | `docs/SUP_TOKENOMICS.md` |
| Utility & policy | `docs/SUPPORT_COIN_UTILITY.md` |

### HMS pack (`HMS_Prelaunch_Brief`) — when lane goes live

| Section | Source file |
|---------|-------------|
| Public roadmap | `docs/HMS_PUBLIC_ROADMAP.md` |
| Tokenomics | `docs/HMS_TOKENOMICS.md` |

### Network deck (`HackMe_Network_Pitch`)

| Section | Source file |
|---------|-------------|
| Slides | `docs/LISTING_PITCH_OUTLINE.md` |
| Ecosystem | `docs/ECOSYSTEM_OVERVIEW.md` |

### Legal pack (`HackMe_Legal_and_Rights`)

| Section | Source file |
|---------|-------------|
| Rights & disclosures | `docs/RIGHTS_AND_DISCLOSURES.md` |
| Trademark & forking | `docs/TRADEMARK_AND_FORKING.md` |

### AGPL license (`AGPL-3.0_License`)

| Section | Source file |
|---------|-------------|
| Full license | `LICENSE` (verbatim export) |

## Deploy PDFs to hackme.tech

```bash
HACKME_DEPLOY_SSH_IDENTITY=~/.ssh/cursor_vps NODE_SSH=hackme-vps \
  SKIP_NODE=1 bash scripts/ops/deploy_hackme_site.sh
```

(`dist/` rsync includes `dist/docs/` when present.)

## Operator checklist

- [ ] Regenerate PDFs after any `economics.go` / SUP genesis change
- [ ] Verify treasury address balance matches transparency page
- [ ] Attach PDFs to exchange portals + link from listing.html
