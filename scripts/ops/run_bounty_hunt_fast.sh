#!/usr/bin/env bash
# Fast bounty hunt — Kleidi + native probes + 4 WASM modules (no heavy cargo genesis).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
HUNT_OUT="${HUNT_OUT:-$ROOT/reports/bounty/hunt-fast-${STAMP}}"
BUDGET_RUNS="${BUDGET_RUNS:-128}"
export PATH="$HOME/.foundry/bin:$PATH"

mkdir -p "$HUNT_OUT"
log() { echo "[hunt-fast] $*" | tee -a "$HUNT_OUT/hunt.log"; }

log "stamp=$STAMP runs=$BUDGET_RUNS"

[[ -d /tmp/kleidi/.git ]] || git clone --depth 1 https://github.com/solidity-labs-io/kleidi.git /tmp/kleidi
[[ -d /tmp/wormhole/.git ]] || git clone --depth 1 https://github.com/wormhole-foundation/wormhole.git /tmp/wormhole

log "kleidi unit tests"
if [[ -x /home/kapa/.local/bin/solc-0.8.25 ]] && command -v forge >/dev/null; then
  (cd /tmp/kleidi && forge test --use /home/kapa/.local/bin/solc-0.8.25 \
    --match-path 'test/unit/*.t.sol' 2>&1 | tee "$HUNT_OUT/kleidi-unit.log" | tail -5) || true
  log "kleidi calldata fuzz"
  (cd /tmp/kleidi && forge test --use /home/kapa/.local/bin/solc-0.8.25 \
    --match-contract CalldataListUnitTest --fuzz-runs 256 2>&1 | tee "$HUNT_OUT/kleidi-fuzz.log" | tail -8) || true
fi

log "wormhole native"
OUT="$HUNT_OUT/native-wormhole" ROUNDS=100000 bash "$ROOT/scripts/ops/immunefi_native_wormhole.sh" >>"$HUNT_OUT/hunt.log" 2>&1 || true

bash "$ROOT/scripts/build_immunefi_pack.sh" >>"$HUNT_OUT/build.log" 2>&1

TARGETS=(wormhole_quorum wormhole_sig_index berachain_denominator berachain_pol)

: >"$HUNT_OUT/wasm.jsonl"
for t in "${TARGETS[@]}"; do
  log "wasm $t"
  mod_out="$HUNT_OUT/wasm/$t"
  OUT="$mod_out" TARGET="$t" STAMP="$STAMP" BUDGET_RUNS="$BUDGET_RUNS" CHECK_SEMANTICS=detector \
    bash "$ROOT/scripts/ops/run_immunefi_pilot.sh" >>"$HUNT_OUT/hunt.log" 2>&1 || log "WARN $t (retry after sleep if SQLITE_BUSY)"
  sleep 2
  [[ -f "$mod_out/summary.json" ]] && jq -c . "$mod_out/summary.json" >>"$HUNT_OUT/wasm.jsonl"
done

python3 - "$HUNT_OUT" "$ROOT" <<'PY'
import json, pathlib, sys
hunt = pathlib.Path(sys.argv[1])
wasm = [json.loads(l) for l in (hunt/"wasm.jsonl").read_text().splitlines() if l.strip()]
kleidi_log = (hunt/"kleidi-unit.log").read_text() if (hunt/"kleidi-unit.log").exists() else ""
kleidi_fuzz = (hunt/"kleidi-fuzz.log").read_text() if (hunt/"kleidi-fuzz.log").exists() else ""
kleidi_ok = "0 failed" in kleidi_log and "tests passed" in kleidi_log
fuzz_ok = "0 failed" in kleidi_fuzz or "passed" in kleidi_fuzz.lower()
native = {}
for p in hunt.glob("native-wormhole*/summary.json"):
    native = json.loads(p.read_text())
rollup = {
    "stamp": hunt.name,
    "wasm_modules": len(wasm),
    "wasm_critical": sum(r.get("critical_count",0) for r in wasm),
    "wasm_guard_signals": sum(r.get("guard_signal_count",0) for r in wasm),
    "kleidi_unit_ok": kleidi_ok,
    "kleidi_fuzz_ok": fuzz_ok,
    "native_wormhole": native,
    "verdict": "NO_BOUNTY_FINDING" if not native.get("panics") else "CANDIDATE_REVIEW",
    "write_to_immunefi": False,
    "wasm_summaries": wasm,
}
(hunt/"rollup.json").write_text(json.dumps(rollup, indent=2)+"\n")
print(json.dumps(rollup, indent=2)[:2000])
PY

log "done → $HUNT_OUT/rollup.json"
