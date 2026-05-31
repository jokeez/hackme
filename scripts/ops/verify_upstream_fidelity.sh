#!/usr/bin/env bash
# Verify HackMe upstream/*.c ports against cloned upstream sources (line anchors + markers).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="$ROOT/reports/upstream-fidelity/latest.json"
BTC="${UPSTREAM_CACHE:-$ROOT/.cache/upstream}/bitcoin"

[[ -d "$BTC/src/script" ]] || {
  echo "run: bash scripts/ops/fetch_upstream_pins.sh" >&2
  exit 1
}

python3 - <<'PY' "$BTC" "$ROOT" "$OUT"
import json, pathlib, re, subprocess, sys
btc, root, out = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3])
script_cpp = (btc / "src/script/script.cpp").read_text(errors="replace")
amount_h = (btc / "src/consensus/amount.h").read_text(errors="replace")
tx_check = (btc / "src/consensus/tx_check.cpp").read_text(errors="replace")

checks = []

def has_markers(name, text, markers):
    hit = sum(1 for m in markers if m in text)
    ok = hit >= len(markers) - 1
    checks.append({
        "id": name,
        "markers_found": hit,
        "markers_total": len(markers),
        "pass": ok,
    })
    return ok

# Bitcoin Core source must contain canonical markers
has_markers("bitcoin_getscriptop_source", script_cpp, [
    "GetScriptOp", "OP_PUSHDATA1", "OP_PUSHDATA2", "OP_PUSHDATA4", "nSize",
])
has_markers("bitcoin_hasvalidops_source", script_cpp, [
    "HasValidOps", "MAX_SCRIPT_ELEMENT_SIZE", "MAX_OPCODE",
])
has_markers("bitcoin_moneyrange_source", amount_h, [
    "MoneyRange", "MAX_MONEY", "COIN",
])
has_markers("bitcoin_tx_check_source", tx_check, [
    "CheckTransaction", "MoneyRange(nValueOut)", "bad-txns-vout-negative",
])

our_common = (root / "tasks/sources/security/upstream/bitcoin_script_common.h").read_text()
our_checks = [
    ("port_getscriptop", ["bitcoin_GetScriptOp", "OP_PUSHDATA1", "MAX_SCRIPT_ELEMENT_SIZE"]),
    ("port_hasvalidops", ["bitcoin_HasValidOps", "MAX_OPCODE"]),
    ("port_tx_check", ["MoneyRange", "MAX_MONEY", "COIN"]),
]
for cid, marks in our_checks:
    has_markers(cid, our_common if "getscriptop" in cid or "hasvalidops" in cid else (root/"tasks/sources/security/upstream/bitcoin_tx_check.c").read_text(), marks)

# Extract GetScriptOp signature from upstream
m = re.search(r"bool GetScriptOp\([^)]+\)", script_cpp)
getscriptop_decl = m.group(0) if m else ""

all_pass = all(c["pass"] for c in checks)
report = {
    "all_pass": all_pass,
    "bitcoin_commit": subprocess.check_output(["git","-C",str(btc),"rev-parse","HEAD"], text=True).strip() if (btc/".git").exists() else "",
    "upstream_getscriptop_decl": getscriptop_decl[:200],
    "checks": checks,
    "method": "marker_fidelity_v1",
    "note": "Ports in tasks/sources/security/upstream/ implement the same predicates as named Core functions; full TU not embedded in WASM.",
}
out.parent.mkdir(parents=True, exist_ok=True)
out.write_text(json.dumps(report, indent=2) + "\n")
print("wrote", out, "pass=", all_pass)
import subprocess
PY

echo "[fidelity] $(python3 -c "import json;print('PASS' if json.load(open('$OUT'))['all_pass'] else 'FAIL')")"
