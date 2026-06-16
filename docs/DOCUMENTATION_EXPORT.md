# Documentation export — PDF / DOCX per ticker

HackMe maintains **Markdown as source of truth** on GitHub and **HTML on hackme.tech**. Exchange DD and investors often need **PDF or DOCX** attachments. This doc defines the repeatable export layout.

## Per-ticker document sets

### HMC pack (`HMC_Listing_Pack`)

| Section | Source file |
|---------|-------------|
| Cover + memo | `docs/EXCHANGE_LISTING_MEMO.md` |
| Tokenomics | `docs/HMC_TOKENOMICS.md` |
| Allocation & vesting | `docs/TOKEN_ALLOCATION_AND_VESTING.md` (HMC table) |
| Technical integration | `docs/EXCHANGE_LISTING_WALLET_PREP.md` |
| Chain spec excerpt | `spec/CHAIN_SPEC.md` |

### SUP pack (`SUP_Companion_Overview`)

| Section | Source file |
|---------|-------------|
| Utility & policy | `docs/SUPPORT_COIN_UTILITY.md` |
| Tokenomics | `docs/SUP_TOKENOMICS.md` |
| Allocation | `docs/TOKEN_ALLOCATION_AND_VESTING.md` (SUP table) |
| Phase C status | `docs/SUP_PHASE_C.md` |

### HMS pack (`HMS_Prelaunch_Brief`) — when lane goes live

| Section | Source file |
|---------|-------------|
| Public roadmap | `docs/HMS_PUBLIC_ROADMAP.md` |
| Tokenomics | `docs/HMS_TOKENOMICS.md` |
| Allocation | `docs/TOKEN_ALLOCATION_AND_VESTING.md` (HMS table) |

### Network deck (`HackMe_Network_Pitch`)

| Section | Source file |
|---------|-------------|
| Slides | `docs/LISTING_PITCH_OUTLINE.md` |
| Ecosystem | `docs/ECOSYSTEM_OVERVIEW.md` |

## Recommended export workflow

```bash
# Option A — pandoc (local)
cd /path/to/hackme
pandoc docs/EXCHANGE_LISTING_MEMO.md docs/HMC_TOKENOMICS.md \
  docs/TOKEN_ALLOCATION_AND_VESTING.md \
  -o dist/docs/HMC_Listing_Pack.pdf --pdf-engine=xelatex -V geometry:margin=1in

pandoc docs/SUP_TOKENOMICS.md docs/SUPPORT_COIN_UTILITY.md \
  -o dist/docs/SUP_Companion_Overview.pdf --pdf-engine=xelatex

# Option B — DOCX for exchange forms
pandoc docs/HMC_TOKENOMICS.md -o dist/docs/HMC_Tokenomics.docx
```

## Site attachment URLs (future)

After PDF build + deploy to `dist/docs/`:

- `https://hackme.tech/dist/docs/HMC_Listing_Pack.pdf`
- `https://hackme.tech/dist/docs/SUP_Companion_Overview.pdf`

Link from [listing.html](https://hackme.tech/listing.html) and per-coin pages.

## Versioning

- Bump `updated` in each markdown when economics change
- Include `GET /api/status` → `policy_hash` in PDF footer
- Match release channel (`scripts/release/CURRENT_VERSION`)

## Operator checklist

- [ ] Regenerate PDFs after any `economics.go` / SUP genesis change
- [ ] Verify treasury address balance matches transparency page
- [ ] Upload to exchange portals + link from listing.html
