#!/usr/bin/env bash
# Bounty autopilot — max projects, max fuzz, minimal human input.
#
#   bash scripts/ops/run_bounty_autopilot.sh
#   FAST=1 bash scripts/ops/run_bounty_autopilot.sh          # halve fuzz budgets
#   SKIP_PHASES=immunefi_wasm,native_wormhole bash scripts/ops/run_bounty_autopilot.sh
set -euo pipefail
export PATH="/home/kapa/.nvm/versions/node/v24.14.1/bin:/home/kapa/.local/bin:$HOME/.foundry/bin:$HOME/.cargo/bin:$PATH"
export FOUNDRY_OFFLINE="${FOUNDRY_OFFLINE:-true}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/bounty/autopilot-${STAMP}}"
REGISTRY="${REGISTRY:-$ROOT/upstream/bounty_fuzz_registry.json}"
SKIP_PHASES="${SKIP_PHASES:-}"

mkdir -p "$OUT/phases"
log() { echo "[autopilot $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/autopilot.log"; }

phase_enabled() {
  local id="$1"
  [[ ",${SKIP_PHASES}," == *",${id},"* ]] && return 1
  python3 - "$REGISTRY" "$id" <<'PY'
import json, sys
reg, pid = sys.argv[1], sys.argv[2]
phases = {p["id"]: p for p in json.load(open(reg)).get("phases", [])}
print("1" if phases.get(pid, {}).get("enabled", True) else "0")
PY
}

run_phase() {
  local id="$1" script="$2"
  phase_enabled "$id" | grep -q 1 || { log "skip phase $id"; return 0; }
  log "=== phase: $id ==="
  local phase_out="$OUT/phases/$id"
  mkdir -p "$phase_out"
  set +e
  OUT="$phase_out" STAMP="${STAMP}" bash "$ROOT/$script" >>"$OUT/autopilot.log" 2>&1
  local rc=$?
  set -e
  echo "$rc" >"$phase_out/exit.code"
  log "phase $id rc=$rc"
  return 0
}

# Fuzz budgets
if [[ "${FAST:-0}" == "1" ]]; then
  export BUDGET=15000 TIME_LIMIT=180
  export FUZZ_RUNS=4096 UPSTREAM_FUZZ_RUNS=2048 CUSTOM_FUZZ=4096 KLEIDI_FUZZ=4096
  export FOUNDRY_FUZZ_RUNS=1024 BUDGET_RUNS=256
  export ROUNDS=100000
else
  export BUDGET=30000 TIME_LIMIT=300
  export FUZZ_RUNS=16384 UPSTREAM_FUZZ_RUNS=8192 CUSTOM_FUZZ=8192 KLEIDI_FUZZ=8192
  export FOUNDRY_FUZZ_RUNS=2048 BUDGET_RUNS=512
  export ROUNDS=300000
fi

export SKIP_IDS="$(python3 -c "import json; print(','.join(json.load(open('$REGISTRY'))['skip_disclosure_hold']))")"
export TOKENIZE_PIN="52b0322fb566c7143d09c23b7bd30f2e092e0691"
export SKIP_FORK=1

log "start out=$OUT fast=${FAST:-0} skip=$SKIP_PHASES"

run_phase oss_cve       "scripts/ops/run_oss_cve_nightly.sh"
run_phase tokenize_ultra "scripts/ops/run_tokenize_ultra_hunt.sh"
run_phase foundry_open  "scripts/ops/run_bounty_open_max.sh"
run_phase hackenproof_lowtier "scripts/ops/run_hackenproof_lowtier_push.sh"
run_phase immunefi_wasm "scripts/ops/run_bounty_hunt_fast.sh"
run_phase native_wormhole "scripts/ops/immunefi_native_wormhole.sh"
run_phase discovery_fuzz "scripts/ops/run_bounty_discovery_fuzz.sh"

python3 - "$OUT" "$REGISTRY" "$STAMP" <<'PY'
import json, pathlib, sys, time

out = pathlib.Path(sys.argv[1])
registry = json.loads(pathlib.Path(sys.argv[2]).read_text())
stamp = sys.argv[3]

phases = {}
candidates = []
for d in sorted((out / "phases").iterdir()):
    if not d.is_dir():
        continue
    pid = d.name
    exit_code = int((d / "exit.code").read_text().strip()) if (d / "exit.code").exists() else -1
    rollup_paths = list(d.glob("rollup.json")) + list(d.glob("ROLLUP.json"))
    detail = {"exit_code": exit_code}
    for rp in rollup_paths:
        try:
            r = json.loads(rp.read_text())
            detail["rollup"] = r
            if r.get("verdict") in ("CVE_CANDIDATE", "BOUNTY_CANDIDATE", "REVIEW_INTERESTING", "REVIEW_FAILS"):
                candidates.append({"phase": pid, "verdict": r.get("verdict"), "path": str(rp)})
            for c in r.get("candidates") or r.get("interesting") or r.get("cve_candidates") or []:
                candidates.append({"phase": pid, "item": c})
        except Exception:
            pass
    phases[pid] = detail

verdict = "BOUNTY_CANDIDATE" if candidates else "NO_BOUNTY_FINDING"
rollup = {
    "stamp": out.name,
    "time_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "strategy": "bounty_autopilot_max_projects",
    "registry": str(pathlib.Path(sys.argv[2]).name),
    "phases": phases,
    "candidates": candidates[:50],
    "verdict": verdict,
    "projects_fuzzed": list(phases.keys()),
    "foundry_open": registry.get("foundry_open", []),
    "next": [
        "Review candidates in reports/bounty/autopilot-*/phases/",
        "HackenProof: tokenize.it ends 2026-06-27",
        "OSS CVE: disclosure hold on centijson/libucl/cfgpack",
    ],
}
(out / "rollup.json").write_text(json.dumps(rollup, indent=2) + "\n")
html = f"""<!DOCTYPE html><html><head><meta charset=utf-8><title>Bounty Autopilot</title>
<style>body{{font-family:system-ui;max-width:960px;margin:2rem auto;padding:0 1rem}}
.ok{{color:#080}}.warn{{color:#a60}}.bad{{color:#c00}}
table{{border-collapse:collapse;width:100%}}td,th{{border:1px solid #ccc;padding:6px;font-size:13px}}</style></head><body>
<h1>Bounty Autopilot</h1><p><strong>Verdict:</strong> <span class="{'bad' if verdict != 'NO_BOUNTY_FINDING' else 'ok'}">{verdict}</span></p>
<p>{time.strftime('%Y-%m-%d %H:%M UTC')}</p>
<h2>Phases</h2><table><tr><th>Phase</th><th>Exit</th></tr>"""
for pid, d in phases.items():
    html += f"<tr><td>{pid}</td><td>{d.get('exit_code')}</td></tr>"
html += "</table></body></html>"
(out / "index.html").write_text(html)
print(json.dumps({"verdict": verdict, "phases": list(phases.keys()), "candidates": len(candidates)}, indent=2))
PY

ln -sfn "$OUT" "$ROOT/reports/bounty/CURRENT_AUTOPILOT"
log "done → $OUT/rollup.json"
