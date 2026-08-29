#!/usr/bin/env bash
# Tokenize.it HackenProof ultra hunt — pinned audit commit + max Foundry fuzz.
#
#   bash scripts/ops/run_tokenize_ultra_hunt.sh
#   FUZZ_RUNS=32768 SKIP_FORK=1 bash scripts/ops/run_tokenize_ultra_hunt.sh
set -euo pipefail
export PATH="/home/kapa/.nvm/versions/node/v24.14.1/bin:/home/kapa/.local/bin:$HOME/.foundry/bin:$PATH"
export FOUNDRY_OFFLINE="${FOUNDRY_OFFLINE:-true}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PIN="${TOKENIZE_PIN:-52b0322fb566c7143d09c23b7bd30f2e092e0691}"
REPO="${TOKENIZE_REPO:-$ROOT/.cache/bounty-repos/tokenize-it}"
LAB="$ROOT/bounty-lab/tokenize-it"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/bounty/tokenize-ultra-${STAMP}}"
FUZZ="${FUZZ_RUNS:-16384}"
UPSTREAM_FUZZ="${UPSTREAM_FUZZ_RUNS:-8192}"
SKIP_FORK="${SKIP_FORK:-1}"

mkdir -p "$OUT"
log() { echo "[tokenize-ultra $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/hunt.log"; }

clone_pin() {
  mkdir -p "$(dirname "$REPO")"
  if [[ ! -d "$REPO/.git" ]]; then
    log "clone tokenize.it"
    git clone --recurse-submodules https://github.com/corpus-io/tokenize.it-smart-contracts.git "$REPO"
  fi
  cd "$REPO"
  git fetch origin "$PIN" 2>/dev/null || git fetch --depth 1 origin "$PIN" 2>/dev/null || true
  git checkout -f "$PIN"
  git submodule update --init --recursive
  log "pinned HEAD=$(git rev-parse --short HEAD)"
}

prepare() {
  cd "$REPO"
  [[ -d lib/forge-std ]] || forge install foundry-rs/forge-std --no-commit >>"$OUT/prepare.log" 2>&1 || true
  if [[ ! -d node_modules/@openzeppelin ]]; then
    log "npm install"
    npm install --legacy-peer-deps >>"$OUT/prepare.log" 2>&1
  fi
  mkdir -p test/hackme
  cp -f "$LAB/HackMe_CoinvestedUltra.t.sol" "$LAB/HackMe_ExitUltra.t.sol" test/hackme/
  log "deployed HackMe ultra tests (2 files)"
}

forge_summary() {
  local id="$1"
  shift
  local logf="$OUT/${id}.log"
  log "forge $id $*"
  set +e
  (cd "$REPO" && eval "forge test $*" 2>&1 | tee "$logf")
  local rc=$?
  set -e
  python3 - "$logf" "$id" "$OUT/${id}.json" "$rc" <<'PY'
import json, pathlib, re, sys
logf, rid, out, rc = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
text = pathlib.Path(logf).read_text(errors="replace") if pathlib.Path(logf).exists() else ""
m = re.search(r"(\d+) tests? passed,\s*(\d+) failed", text)
passed = int(m.group(1)) if m else 0
failed = int(m.group(2)) if m else 0
fail_lines = [ln for ln in text.splitlines() if "[FAIL" in ln][:25]
compile_err = "Compiler run failed" in text or "Error (" in text and "test" not in text.lower()
result = {
    "id": rid, "passed": passed, "failed": failed, "exit_code": rc,
    "compile_err": compile_err, "ok": failed == 0 and not compile_err and passed > 0,
    "fail_snippets": fail_lines,
}
pathlib.Path(out).write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result))
PY
}

log "start pin=$PIN fuzz=$FUZZ upstream_fuzz=$UPSTREAM_FUZZ out=$OUT"
clone_pin
prepare

# --- HackMe custom (audit-focused) ---
forge_summary hackme_ultra \
  "--match-contract 'HackMe_CoinvestedUltra|HackMe_ExitUltra' --fuzz-runs $FUZZ -vv"

# --- Upstream ultra (in-scope contracts) ---
MATCHES=(
  "test/CoinvestedPosition*.t.sol"
  "test/CoinvestedPositionExit.t.sol"
  "test/CoinvestedPositionDistribution.t.sol"
  "test/CoinvestedPositionPullPayouts.t.sol"
  "test/CoinvestedPositionERC2771.t.sol"
  "test/GlobalTokenExitRegistry.t.sol"
  "test/Exit.t.sol"
  "test/Distribution.t.sol"
  "test/Crowdinvesting.t.sol"
  "test/PrivateOffer.t.sol"
  "test/TokenSwap.t.sol"
  "test/TimeLock.t.sol"
  "test/Vesting.t.sol"
  "test/AllowList.t.sol"
)

for pat in "${MATCHES[@]}"; do
  id="upstream_$(echo "$pat" | tr '/.*' '__' | tr '[:upper:]' '[:lower:]')"
  forge_summary "$id" "--match-path '$pat' --fuzz-runs $UPSTREAM_FUZZ -vv" || true
done

python3 - "$OUT" "$PIN" <<'PY'
import json, pathlib, sys, time
out = pathlib.Path(sys.argv[1])
pin = sys.argv[2]
suites = {}
fails = []
interesting = []
for p in sorted(out.glob("*.json")):
    if p.name == "rollup.json":
        continue
    r = json.loads(p.read_text())
    suites[r["id"]] = r
    if not r.get("ok"):
        fails.append(r["id"])
    for ln in r.get("fail_snippets", []):
        if "testFuzz" in ln and "[FAIL" in ln:
            interesting.append({"suite": r["id"], "line": ln[:200]})

verdict = "REVIEW_FAILS" if interesting else ("BUILD_ISSUES" if fails else "NO_BOUNTY_FINDING")
rollup = {
    "stamp": out.name,
    "time_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "platform": "hackenproof",
    "program": "tokenize-it-token-sc-dualdefense-audit",
    "repo_pin": pin,
    "repo_url": f"https://github.com/corpus-io/tokenize.it-smart-contracts/tree/{pin}",
    "suites": suites,
    "failed_suites": fails,
    "interesting_fuzz_fails": interesting[:30],
    "verdict": verdict,
    "submit_ready": bool(interesting),
    "note": "Foundry fuzz fail ≠ bounty — triage counterexample manually before HackenProof submit",
}
(out / "rollup.json").write_text(json.dumps(rollup, indent=2) + "\n")
html = f"""<!DOCTYPE html><html><head><meta charset=utf-8><title>Tokenize.it Ultra Hunt</title>
<style>body{{font-family:system-ui;max-width:960px;margin:2rem auto;padding:0 1rem}}
table{{border-collapse:collapse;width:100%}}td,th{{border:1px solid #ccc;padding:6px;font-size:13px}}
.fail{{color:#c00}}.ok{{color:#080}}</style></head><body>
<h1>Tokenize.it Ultra Hunt</h1>
<p><strong>Pin:</strong> <code>{pin}</code></p>
<p><strong>Verdict:</strong> <span class="{'fail' if verdict != 'NO_BOUNTY_FINDING' else 'ok'}">{verdict}</span></p>
<h2>Suites</h2><table><tr><th>ID</th><th>Pass</th><th>Fail</th><th>OK</th></tr>"""
for sid, r in sorted(suites.items()):
    html += f"<tr><td>{sid}</td><td>{r.get('passed')}</td><td>{r.get('failed')}</td><td>{r.get('ok')}</td></tr>"
html += "</table></body></html>"
(out / "index.html").write_text(html)
print(json.dumps({"verdict": verdict, "failed": fails, "interesting": len(interesting)}, indent=2))
PY

ln -sfn "$OUT" "$ROOT/reports/bounty/CURRENT_TOKENIZE"
log "done → $OUT/rollup.json"
cat "$OUT/rollup.json" | head -40
