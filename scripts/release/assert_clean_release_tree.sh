#!/usr/bin/env bash
# Fail closed if the tree is dirty or HEAD is not on origin/main.
# Used by make_release_bundle + run_full_release_local.
# Override only for throwaway local builds: ALLOW_DIRTY_RELEASE=1.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

if [[ "${ALLOW_DIRTY_RELEASE:-0}" == "1" ]]; then
  echo "[clean-release] ALLOW_DIRTY_RELEASE=1 — skipping dirty/VCS gates"
  exit 0
fi

dirty="$(git status --porcelain --untracked-files=no 2>/dev/null || true)"
if [[ -n "$dirty" ]]; then
  echo "[clean-release] ERROR: uncommitted changes — commit or stash them first:" >&2
  echo "$dirty" | head -20 >&2
  exit 1
fi

git fetch -q origin main 2>/dev/null || echo "[clean-release] WARN: could not fetch origin/main" >&2

if ! git merge-base --is-ancestor HEAD origin/main 2>/dev/null; then
  echo "[clean-release] ERROR: HEAD $(git rev-parse --short HEAD) is not on origin/main — push it first" >&2
  exit 1
fi

behind="$(git rev-list --count HEAD..origin/main 2>/dev/null || echo 0)"
if [[ "$behind" != "0" ]]; then
  echo "[clean-release] ERROR: HEAD is $behind commit(s) behind origin/main — git pull --rebase first" >&2
  exit 1
fi

echo "[clean-release] OK commit=$(git rev-parse --short HEAD)"
