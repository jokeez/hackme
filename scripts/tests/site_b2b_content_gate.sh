#!/usr/bin/env bash
# Local gate: B2B fuzz copy on static site (no prod HTTP required).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

SITE="$ROOT/web/site"
failures=0

require_in() {
  local file="$1" needle="$2" label="$3"
  if grep -qF "$needle" "$file"; then
    pass "$label"
  else
    fail_msg "$label missing in $(basename "$file")"
    failures=$((failures + 1))
  fi
}

must_not_in() {
  local file="$1" needle="$2" label="$3"
  if grep -qF "$needle" "$file"; then
    fail_msg "$label still in $(basename "$file")"
    failures=$((failures + 1))
  else
    pass "$label absent"
  fi
}

echo "[site-b2b-content] checking static copy"
require_in "$SITE/developers.html" "hackme-fuzzing wizard" "developers wizard"
require_in "$SITE/developers.html" "Before (v3)" "developers before/after table"
require_in "$SITE/developers.html" "B2B packages" "developers packages table"
require_in "$SITE/downloads.html" "wizard --wasm" "downloads wizard"
require_in "$SITE/fuzz-guide.html" "B2B customer wizard" "fuzz-guide wizard section"
require_in "$SITE/fuzz-campaigns.html" "hackme-fuzzing wizard" "fuzz-campaigns wizard"
require_in "$SITE/developers.html" "bounds_smoke" "developers scan smoke packs"
require_in "$SITE/developers.html" "hunt_heavy" "developers hunt heavy package"
require_in "$SITE/developers.html" "severity-tier bounty" "developers hunt severity tiers"
require_in "$SITE/developers.html" "cross-campaign persist" "developers corpus persist"
require_in "$SITE/orders.html" "Hunt Heavy" "orders hunt heavy pill"
require_in "$SITE/orders.html" "50/50 escrow" "orders hunt escrow pill"
require_in "$SITE/assets/news.json" "2026-08-22-release-rc15-b2b-fuzz-phase2" "news b2b fuzz item"

must_not_in "$SITE/api-reference.html" "Treasury balance for fuzzing escrow (public on hackme.tech)" "api-ref stale treasury"
must_not_in "$SITE/assets/fuzzing-console.js" "Developer Dashboard" "console stale dashboard label"

python3 -m json.tool "$SITE/assets/news.json" >/dev/null || { fail_msg "news.json invalid JSON"; failures=$((failures + 1)); }
python3 -m json.tool "$SITE/assets/news-feed.json" >/dev/null || { fail_msg "news-feed.json invalid JSON"; failures=$((failures + 1)); }

if [[ "$failures" -gt 0 ]]; then
  fail "site_b2b_content_gate FAIL ($failures)"
fi
pass "site_b2b_content_gate PASS"
