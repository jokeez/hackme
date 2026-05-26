#!/usr/bin/env bash
# Publish rc11i release artifacts (incl. HackMe OS ISO) + site to canonical VPS.
#   NODE_SSH=hackme-vps bash scripts/ops/deploy_release_rc11i_iso.sh
set -euo pipefail
VERSION="${VERSION:-0.1.0-rc11i}" exec "$(dirname "$0")/deploy_release_rc11g_iso.sh"
