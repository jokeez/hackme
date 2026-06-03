#!/usr/bin/env bash
# Publish rc11k release artifacts (incl. HackMe OS ISO) + site to canonical VPS.
#   NODE_SSH=hackme-vps bash scripts/ops/deploy_release_rc11k_iso.sh
VERSION="${VERSION:-0.1.0-rc11k}" exec "$(dirname "$0")/deploy_release_rc11g_iso.sh"
