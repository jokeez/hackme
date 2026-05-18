#!/usr/bin/env bash
set -euo pipefail

BASE="${BASE:-http://127.0.0.1:8080}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST_DIR="$ROOT_DIR/tasks/manifests/security"

if [[ ! -d "$MANIFEST_DIR" ]]; then
  echo "Missing manifests directory: $MANIFEST_DIR" >&2
  echo "Run: bash scripts/build_security_task_pack.sh" >&2
  exit 1
fi

shopt -s nullglob
files=("$MANIFEST_DIR"/*.json)
if [[ ${#files[@]} -eq 0 ]]; then
  echo "No manifests found in $MANIFEST_DIR" >&2
  echo "Run: bash scripts/build_security_task_pack.sh" >&2
  exit 1
fi

for f in "${files[@]}"; do
  echo "Submitting $(basename "$f")"
  curl -s -X POST "$BASE/api/tasks" \
    -H "Content-Type: application/json" \
    --data-binary @"$f" | jq
  echo
done

echo "Current security tasks:"
curl -s "$BASE/api/tasks" | jq '.tasks[] | select(.id | startswith("order-rust-") or startswith("order-cpp-")) | {id,status,target_solves,progress_count,progress_pct,artifact_hash}'
