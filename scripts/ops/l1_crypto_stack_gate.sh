#!/usr/bin/env bash
# Gate: L1 crypto stack report exists and probes clean.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

HTML="$ROOT/web/site/reports/l1-crypto-stack.html"
JSON="$ROOT/reports/l1-crypto-stack/latest.json"

[[ -f "$HTML" ]] || { echo "[l1stack-gate] missing $HTML — run: bash scripts/ops/run_l1_crypto_stack_research.sh" >&2; exit 1; }
[[ -f "$JSON" ]] || { echo "[l1stack-gate] missing $JSON" >&2; exit 1; }
[[ -f "$ROOT/tasks/artifacts/security/upstream_bitcoin_getscriptop.wasm" ]] || {
  echo "[l1stack-gate] missing upstream WASM — run: bash scripts/build_upstream_l1_pack.sh" >&2
  exit 1
}

python3 - <<'PY'
import json, pathlib, sys
j = json.loads(pathlib.Path("reports/l1-crypto-stack/latest.json").read_text())
if not j.get("all_clean"):
    print("FAIL: traps in probe", file=sys.stderr)
    sys.exit(1)
if len(j.get("modules") or []) < 5:
    print("FAIL: expected 5 modules", file=sys.stderr)
    sys.exit(1)
print("ok chains:", [m["chain"] for m in j["modules"]])
PY

echo "[l1stack-gate] PASS"
