#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
JSON="$ROOT/reports/l1-crypto-stack-v3/latest.json"
REPRO_JSON="$ROOT/reports/l1-crypto-stack-v3/repro_bundle.json"
HTML="$ROOT/web/site/reports/l1-crypto-stack-v3.html"
FID="$ROOT/reports/upstream-fidelity/latest.json"

[[ -f "$JSON" ]] || { echo "missing $JSON" >&2; exit 1; }
[[ -f "$REPRO_JSON" ]] || { echo "missing $REPRO_JSON" >&2; exit 1; }
[[ -f "$HTML" ]] || { echo "missing $HTML" >&2; exit 1; }
[[ -f "$FID" ]] || { echo "missing fidelity" >&2; exit 1; }

python3 - <<PY
import json, pathlib, sys
j = json.loads(pathlib.Path("$JSON").read_text())
rb = json.loads(pathlib.Path("$REPRO_JSON").read_text())
f = json.loads(pathlib.Path("$FID").read_text())
if not f.get("all_pass"):
    print("FAIL fidelity", file=sys.stderr); sys.exit(1)
if j.get("golden",{}).get("status") != "pass":
    print("FAIL golden", file=sys.stderr); sys.exit(1)
if j.get("traps_total", 0) != 0:
    print("FAIL wasm traps", file=sys.stderr); sys.exit(1)
corpora = j.get("corpora") or []
if not corpora:
    print("FAIL no official corpus probed", file=sys.stderr); sys.exit(1)
for c in corpora:
    if c.get("files_probed", 0) < 100:
        print("FAIL corpus too small", c.get("corpus_name"), file=sys.stderr); sys.exit(1)
if (rb.get("verdict") or {}).get("crash_traps", 0) != 0:
    print("FAIL repro crash traps > 0", file=sys.stderr); sys.exit(1)
print("ok corpora:", [c["corpus_name"] for c in corpora])
PY
echo "[l1-v3-gate] PASS"
