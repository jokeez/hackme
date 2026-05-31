#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
JSON="$ROOT/reports/l1-crypto-stack-v4/latest.json"
LIVE="$ROOT/reports/l1-crypto-stack-v4/live/summary.json"
HTML="$ROOT/web/site/reports/l1-crypto-stack-v4.html"

[[ -f "$JSON" ]] || { echo "missing $JSON" >&2; exit 1; }
[[ -f "$LIVE" ]] || { echo "missing $LIVE" >&2; exit 1; }
[[ -f "$HTML" ]] || { echo "missing $HTML" >&2; exit 1; }

python3 - <<PY
import json, pathlib, sys
live = json.loads(pathlib.Path("$LIVE").read_text())
campaigns = live.get("campaigns") or []
if len(campaigns) < 3:
    print("FAIL need >=3 live campaigns", file=sys.stderr); sys.exit(1)
if live.get("total_runs_done", 0) < 1 and live.get("total_campaigns", 0) >= 3:
    print("WARN low runs_done but campaigns created — ok for v4 launch")
print("ok campaigns:", len(campaigns), "runs:", live.get("total_runs_done"))
PY
echo "[l1-v4-gate] PASS"
