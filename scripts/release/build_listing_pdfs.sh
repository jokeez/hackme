#!/usr/bin/env bash
# Build HackMe documentation PDF packs (pandoc + xelatex + branded header).
#
# Usage:
#   bash scripts/release/build_listing_pdfs.sh
#
# Requires: pandoc, texlive-xetex, fonts-dejavu (DejaVu Sans)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
OUT="${ROOT}/dist/docs"
BUILD="${OUT}/.build"
PANDOC_DIR="${ROOT}/scripts/release/pandoc"
HEADER="${PANDOC_DIR}/hackme-header.tex"
MAINFONT="${PANDOC_MAINFONT:-DejaVu Sans}"

require() {
  command -v "$1" >/dev/null || {
    echo "[docs-pdf] missing: $1" >&2
    exit 1
  }
}

require pandoc
require xelatex

if ! fc-list | grep -qi 'dejavu sans'; then
  echo "[docs-pdf] WARN: DejaVu Sans not found — install fonts-dejavu-core" >&2
fi

mkdir -p "$OUT" "$BUILD"

PANDOC_PDF=(
  pandoc
  --pdf-engine=xelatex
  --include-in-header="$HEADER"
  -V "mainfont=${MAINFONT}"
  -V geometry:margin=1in
  -V fontsize=11pt
  -V documentclass=article
  -V colorlinks=true
)

build_pdf() {
  local out_name="$1"
  local meta="$2"
  shift 2
  echo "[docs-pdf] ${out_name}"
  "${PANDOC_PDF[@]}" \
    --metadata-file="$meta" \
    "$@" \
    -o "${OUT}/${out_name}"
}

gen_agpl_body() {
  local dest="$1"
  {
    echo ""
    echo "## HackMe copyright notice"
    echo ""
    head -n 9 LICENSE | sed 's/^/    /'
    echo ""
    echo "## Full AGPL-3.0 license text"
    echo ""
    echo '```{=latex}'
    echo '\begin{small}'
    echo '\begin{verbatim}'
    tail -n +11 LICENSE
    echo '\end{verbatim}'
    echo '\end{small}'
    echo '```'
  } >"$dest"
}

build_pdf HMC_Listing_Pack.pdf "${PANDOC_DIR}/meta/hmc.yaml" \
  docs/EXCHANGE_LISTING_MEMO.md \
  docs/HMC_TOKENOMICS.md \
  docs/TOKEN_ALLOCATION_AND_VESTING.md

build_pdf SUP_Companion_Overview.pdf "${PANDOC_DIR}/meta/sup.yaml" \
  docs/SUP_TOKENOMICS.md \
  docs/SUPPORT_COIN_UTILITY.md

build_pdf HackMe_Network_Pitch.pdf "${PANDOC_DIR}/meta/pitch.yaml" \
  docs/LISTING_PITCH_OUTLINE.md \
  docs/ECOSYSTEM_OVERVIEW.md

build_pdf HackMe_Legal_and_Rights.pdf "${PANDOC_DIR}/meta/legal.yaml" \
  docs/RIGHTS_AND_DISCLOSURES.md \
  docs/TRADEMARK_AND_FORKING.md

AGPL_MD="${BUILD}/agpl-body.md"
gen_agpl_body "$AGPL_MD"
echo "[docs-pdf] AGPL-3.0_License.pdf"
"${PANDOC_PDF[@]}" \
  --metadata-file="${PANDOC_DIR}/meta/agpl.yaml" \
  -V fontsize=9pt \
  -V geometry:margin=0.85in \
  "$AGPL_MD" \
  -o "${OUT}/AGPL-3.0_License.pdf"

PDFS=(
  HMC_Listing_Pack.pdf
  SUP_Companion_Overview.pdf
  HackMe_Network_Pitch.pdf
  HackMe_Legal_and_Rights.pdf
  AGPL-3.0_License.pdf
)

(
  cd "$OUT"
  sha256sum "${PDFS[@]}" > SHA256SUMS-docs.txt
)

echo "[docs-pdf] OK — ${#PDFS[@]} PDFs"
ls -lah "$OUT"/*.pdf "$OUT"/SHA256SUMS-docs.txt
