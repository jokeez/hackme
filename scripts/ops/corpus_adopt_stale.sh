#!/usr/bin/env bash
# Fold stale corpus.raw-merge.* dirs back into persistent libFuzzer corpora
# (same logic as internal/hunt AdoptStaleRawMergeDirs). Use when inode alerts
# fire — never rm -rf merge temps blindly.
#
#   REPO=/opt/hackme-bootstrap bash scripts/ops/corpus_adopt_stale.sh
#   REPO=/opt/hackme-bootstrap bash scripts/ops/corpus_adopt_stale.sh --dry-run
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPO="${REPO:-$ROOT}"
DRY=0
[[ "${1:-}" == "--dry-run" ]] && DRY=1
cd "$ROOT"
args=(-repo "$REPO")
[[ "$DRY" == "1" ]] && args+=(-dry-run)
exec go run ./cmd/corpus-adopt-stale "${args[@]}"
