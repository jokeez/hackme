#!/usr/bin/env bash
# Ensure dist/latest.json exists and is current for the active CURRENT_VERSION.
# Optionally upload as a GitHub Release asset (needs gh + auth).
#
#   bash scripts/ops/publish_latest_json.sh
#   UPLOAD_GH=1 bash scripts/ops/publish_latest_json.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION")}"
DIST="${DIST_DIR:-${ROOT}/dist/release_${VERSION}}"
[[ -d "$DIST" ]] || { echo "[publish-latest] missing $DIST" >&2; exit 2; }

bash "${ROOT}/scripts/release/generate_latest_json.sh" "$DIST"
[[ -f "${ROOT}/dist/latest.json" ]] || { echo "[publish-latest] missing dist/latest.json" >&2; exit 1; }
jq -e '.schema=="hackme.release.latest.v1" and (.version|length)>0' "${ROOT}/dist/latest.json" >/dev/null

# Ensure sig when key present (generate_latest_json already tries; re-sign is idempotent).
if [[ -f "${HACKME_UPDATE_SIGN_KEY:-${ROOT}/.secrets/hackme_update_ed25519.pem}" ]]; then
  bash "${ROOT}/scripts/release/sign_latest_json.sh" "${ROOT}/dist/latest.json"
fi

echo "[publish-latest] OK ${ROOT}/dist/latest.json → $(jq -r .version "${ROOT}/dist/latest.json")"
[[ -f "${ROOT}/dist/latest.json.sig" ]] && echo "[publish-latest] sig present: dist/latest.json.sig"

if [[ "${UPLOAD_GH:-0}" == "1" ]]; then
  command -v gh >/dev/null || { echo "[publish-latest] gh CLI required for UPLOAD_GH=1" >&2; exit 2; }
  echo "[publish-latest] uploading latest.json to GitHub release ${VERSION}"
  gh release upload "$VERSION" "${ROOT}/dist/latest.json" --clobber \
    || gh release upload "$VERSION" "${DIST}/latest.json" --clobber
  if [[ -f "${ROOT}/dist/latest.json.sig" ]]; then
    gh release upload "$VERSION" "${ROOT}/dist/latest.json.sig" --clobber \
      || gh release upload "$VERSION" "${DIST}/latest.json.sig" --clobber || true
  fi
  echo "[publish-latest] also ensure site: NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_site.sh"
fi
