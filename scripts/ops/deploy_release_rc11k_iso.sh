#!/usr/bin/env bash
# Publish rc11k release artifacts (incl. HackMe OS ISO) + site to canonical VPS.
#   NODE_SSH=hackme-vps bash scripts/ops/deploy_release_rc11k_iso.sh
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION" 2>/dev/null || echo 0.1.0-rc11l)}" exec "$(dirname "$0")/deploy_release_rc11g_iso.sh"
