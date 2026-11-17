#!/usr/bin/env bash
# Build HackMe documentation PDF packs (ReportLab branded dark theme).
#
# Usage:
#   bash scripts/release/build_listing_pdfs.sh
#
# Requires: Python 3 + reportlab + Pillow
# Optional venv: scripts/release/pdf_pack/.venv
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
PACK="${ROOT}/scripts/release/pdf_pack"
VENV_PY="${PACK}/.venv/bin/python"

if [[ -x "$VENV_PY" ]]; then
  PY="$VENV_PY"
elif command -v python3 >/dev/null; then
  PY="$(command -v python3)"
else
  echo "[docs-pdf] missing python3" >&2
  exit 1
fi

if ! "$PY" -c "import reportlab, PIL" 2>/dev/null; then
  echo "[docs-pdf] missing reportlab/Pillow — create venv:" >&2
  echo "  python3 -m venv ${PACK}/.venv && ${PACK}/.venv/bin/pip install reportlab Pillow" >&2
  exit 1
fi

echo "[docs-pdf] building branded packs with $PY"
"$PY" "${PACK}/build_all.py"
ls -lah "${ROOT}/dist/docs/"*.pdf "${ROOT}/dist/docs/SHA256SUMS-docs.txt"
