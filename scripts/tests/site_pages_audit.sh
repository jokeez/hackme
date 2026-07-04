#!/usr/bin/env bash
# HTTP 200 smoke for all public site pages (hackme.tech or SITE_BASE).
set -euo pipefail

SITE="${SITE_BASE:-https://hackme.tech}"
fail=0

check() {
  local path="$1"
  local code
  if [[ "$path" == *.pdf ]]; then
    code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 30 -I "${SITE}${path}")"
  else
    code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 45 "${SITE}${path}")"
  fi
  if [[ "$code" == "200" ]]; then
    echo "[site-pages] PASS $path"
  else
    echo "[site-pages] FAIL $path HTTP $code" >&2
    fail=$((fail + 1))
  fi
}

PAGES=(
  /
  /index.html
  /downloads.html
  /docs.html
  /economics-model.html
  /coins.html
  /coin-hmc.html
  /coin-sup.html
  /coin-hms.html
  /token-transparency.html
  /listing.html
  /roadmap.html
  /legal.html
  /legal-eula.html
  /legal-terms.html
  /legal-privacy.html
  /legal-risk.html
  /contacts.html
  /news.html
  /research.html
  /developers.html
  /operator-checklist.html
  /security-rewards.html
  /fuzz-campaigns.html
  /fuzz-guide.html
  /explorer-lite.html
  /fuzz-marketplace.html
  /fuzzing-console.html
  /phasing-console.html
  /security-notes.html
  /sitemap.xml
  /robots.txt
  /reports/bitcoin30.html
  /reports/bitcoin30-day19.html
  /reports/oss-cve/index.html
  /api-reference.html
  /dist/docs/HMC_Listing_Pack.pdf
  /dist/docs/SUP_Companion_Overview.pdf
  /dist/docs/HackMe_Network_Pitch.pdf
  /dist/docs/HackMe_Legal_and_Rights.pdf
  /dist/docs/AGPL-3.0_License.pdf
)

for p in "${PAGES[@]}"; do
  check "$p"
done

if [[ "$fail" -gt 0 ]]; then
  echo "[site-pages] OVERALL: FAIL ($fail)"
  exit 1
fi
echo "[site-pages] OVERALL: PASS (${#PAGES[@]} pages)"
