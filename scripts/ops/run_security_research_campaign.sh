#!/usr/bin/env bash
# Build security WASM pack, lint manifests, optional submit one order to prod/local node.
#
# Usage:
#   bash scripts/ops/run_security_research_campaign.sh
#   BASE=https://hackme.tech DEV_TOKEN=hmdev_... TASK=script_push_bounds_guard BUILD_LANG=rust bash scripts/ops/run_security_research_campaign.sh --submit
#
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

TASK="${TASK:-script_push_bounds_guard}"
# BUILD_LANG avoids clash with system LANG (locale).
BUILD_LANG="${BUILD_LANG:-rust}"
BASE="${BASE:-http://127.0.0.1:8080}"
SUBMIT=0
[[ "${1:-}" == "--submit" ]] && SUBMIT=1

echo "[security-campaign] build WASM + manifests"
bash scripts/build_security_task_pack.sh

echo "[security-campaign] ABI + manifest lint"
go run ./tools/task_abi_check "$ROOT/tasks/artifacts/security/${BUILD_LANG}_${TASK}.wasm"
go run ./tools/task_manifest_lint "$ROOT/tasks/manifests/security/order-${BUILD_LANG}-${TASK}-001.json"

MANIFEST="$ROOT/tasks/manifests/security/order-${BUILD_LANG}-${TASK}-001.json"
echo "[security-campaign] manifest: $MANIFEST"
jq -c '{id, artifact_hash, wasm_artifact_path, target_solves, reward_hmc}' "$MANIFEST"

if [[ "$SUBMIT" -eq 0 ]]; then
  echo
  echo "Next: fund treasury, then:"
  echo "  DEV_TOKEN=... BASE=https://hackme.tech bash scripts/ops/run_security_research_campaign.sh --submit"
  echo "Or on VPS node with manifest path + admin API."
  exit 0
fi

if [[ -z "${DEV_TOKEN:-}" ]]; then
  echo "[security-campaign] set DEV_TOKEN or HACKME_DEVELOPER_TOKEN" >&2
  exit 1
fi
TOK="${DEV_TOKEN:-${HACKME_DEVELOPER_TOKEN:-}}"
# Inline wasm for remote POST (manifest uses wasm_artifact_path for local node only).
WASM="$ROOT/tasks/artifacts/security/${BUILD_LANG}_${TASK}.wasm"
BODY="$(python3 - <<PY
import json, pathlib
m = json.loads(pathlib.Path("$MANIFEST").read_text())
w = pathlib.Path("$WASM").read_bytes()
m["wasm_check_hex"] = w.hex()
m.pop("wasm_artifact_path", None)
print(json.dumps(m))
PY
)"
echo "[security-campaign] POST $BASE/api/tasks"
curl -fsS -X POST "$BASE/api/tasks" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Developer-Token: $TOK" \
  --data-binary "$BODY" | jq .
