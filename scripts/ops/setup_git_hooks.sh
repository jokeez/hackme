#!/usr/bin/env bash
# Point this repo at version-controlled hooks (strips Cursor co-author trailers).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
chmod +x "$ROOT/.githooks/commit-msg"
git -C "$ROOT" config core.hooksPath .githooks
echo "[setup-git-hooks] core.hooksPath=.githooks"
