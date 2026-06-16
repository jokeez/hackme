#!/usr/bin/env bash
# Build exchange listing PDF packs from docs/*.md (pandoc + xelatex).
#
# Usage:
#   bash scripts/release/build_listing_pdfs.sh
#
# Requires: pandoc, texlive-xetex, fonts-dejavu (DejaVu Sans)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
OUT="${ROOT}/dist/docs"
MAINFONT="${PANDOC_MAINFONT:-DejaVu Sans}"

require() {
  command -v "$1" >/dev/null || {
    echo "[listing-pdf] missing: $1" >&2
    exit 1
  }
}

require pandoc
require xelatex

if ! fc-list | grep -qi 'dejavu sans'; then
  echo "[listing-pdf] WARN: DejaVu Sans not found — install fonts-dejavu-core" >&2
fi

mkdir -p "$OUT"

PANDOC_PDF=(
  pandoc
  --pdf-engine=xelatex
  -V "mainfont=${MAINFONT}"
  -V geometry:margin=1in
  -V fontsize=11pt
  -V documentclass=article
)

echo "[listing-pdf] HMC_Listing_Pack.pdf"
"${PANDOC_PDF[@]}" \
  docs/EXCHANGE_LISTING_MEMO.md \
  docs/HMC_TOKENOMICS.md \
  docs/TOKEN_ALLOCATION_AND_VESTING.md \
  -o "${OUT}/HMC_Listing_Pack.pdf"

echo "[listing-pdf] SUP_Companion_Overview.pdf"
"${PANDOC_PDF[@]}" \
  docs/SUP_TOKENOMICS.md \
  docs/SUPPORT_COIN_UTILITY.md \
  -o "${OUT}/SUP_Companion_Overview.pdf"

(
  cd "$OUT"
  sha256sum HMC_Listing_Pack.pdf SUP_Companion_Overview.pdf > SHA256SUMS-docs.txt
)

echo "[listing-pdf] OK"
ls -lah "$OUT"/*.pdf "$OUT"/SHA256SUMS-docs.txt
