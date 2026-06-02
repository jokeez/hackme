#!/usr/bin/env bash
# Run all instances listed in .secrets/vast/instances.json sequentially.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/vast/ssh_common.sh
source "$SCRIPT_DIR/ssh_common.sh"

require_instances
mapfile -t names < <(jq -r '.instances[].name' "$INSTANCES_JSON")

fail=0
for n in "${names[@]}"; do
  [[ -n "$n" ]] || continue
  echo ""
  echo "========== $n =========="
  if bash "$SCRIPT_DIR/ssh_run_session.sh" "$n"; then
    echo "[matrix] PASS $n"
  else
    echo "[matrix] FAIL $n"
    fail=$((fail + 1))
  fi
done

echo "[matrix] finished fail=$fail / ${#names[@]}"
exit "$fail"
