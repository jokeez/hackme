#!/usr/bin/env bash
# HackenProof low-competition Solidity targets — max Foundry fuzz.
#
#   bash scripts/ops/run_hackenproof_lowtier_push.sh
#   FOUNDRY_FUZZ_RUNS=8192 bash scripts/ops/run_hackenproof_lowtier_push.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export PATH="/home/kapa/.local/bin:$HOME/.foundry/bin:$PATH"
export FOUNDRY_OFFLINE="${FOUNDRY_OFFLINE:-true}"

STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/bounty/hackenproof-lowtier-${STAMP}}"
FUZZ="${FOUNDRY_FUZZ_RUNS:-8192}"
FUZZ_LITE="${FOUNDRY_FUZZ_LITE:-4096}"
SOLC34="${SOLC34:-/home/kapa/.local/bin/solc-0.8.34}"
FORGE_TIMEOUT="${FORGE_TIMEOUT:-2400}"

mkdir -p "$OUT"
log() { echo "[lowtier $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/push.log"; }

clone_repo() {
  local url="$1" dest="$2" extra="${3:-}"
  if [[ -d "$dest/.git" ]]; then
    log "clone skip $dest"
    return 0
  fi
  log "clone $url"
  # Never rm dest while cwd may be inside it — clone from /tmp parent.
  local name
  name="$(basename "$dest")"
  (cd /tmp && rm -rf "$name" && git clone --depth 1 $extra "$url" "$name") >>"$OUT/clone.log" 2>&1
}

run_forge() {
  local id="$1" dir="$2" match="$3" runs="$4" use_solc="${5:-}"
  local logf="$OUT/${id}.log"
  local jsonf="$OUT/${id}.json"
  mkdir -p "$OUT"
  if [[ -f "$jsonf" ]]; then
    if python3 -c "import json; exit(0 if json.load(open('$jsonf')).get('ok') else 1)" 2>/dev/null; then
      log "skip $id (already ok)"
      return 0
    fi
    # Retry compile_err only when FORCE=1
    if [[ "${FORCE:-0}" != "1" ]] && python3 -c "import json; r=json.load(open('$jsonf')); exit(0 if r.get('compile_err') and not r.get('ok') else 1)" 2>/dev/null; then
      log "skip $id (prior compile_err — set FORCE=1 to retry)"
      return 0
    fi
  fi
  log "forge $id fuzz=$runs match=$match timeout=${FORGE_TIMEOUT}s"
  set +e
  if [[ -n "$use_solc" ]]; then
    timeout "$FORGE_TIMEOUT" bash -c \
      "cd '$dir' && forge test --use '$use_solc' --match-path '$match' --fuzz-runs '$runs' -vv" \
      2>&1 | tee "$logf"
  else
    timeout "$FORGE_TIMEOUT" bash -c \
      "cd '$dir' && forge test --match-path '$match' --fuzz-runs '$runs' -vv" \
      2>&1 | tee "$logf"
  fi
  local rc=$?
  set -e
  python3 - "$logf" "$id" "$OUT/${id}.json" "$rc" <<'PY'
import json, pathlib, re, sys
logf, rid, out, rc = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
text = pathlib.Path(logf).read_text(errors="replace") if pathlib.Path(logf).exists() else ""
passed = failed = 0
m = re.search(r"(\d+) tests? passed,\s*(\d+) failed", text)
if m:
    passed, failed = int(m.group(1)), int(m.group(2))
compile_err = any(x in text for x in ("Compiler run failed", "Encountered invalid solc", "Nothing to compile", "No tests found"))
fail_lines = [ln for ln in text.splitlines() if re.search(r"\[FAIL\]|Suite result:.*failed", ln)]
result = {
    "id": rid,
    "passed": passed,
    "failed": failed,
    "compile_err": compile_err,
    "exit_code": rc,
    "ok": failed == 0 and not compile_err and passed > 0,
    "fail_snippets": fail_lines[:20],
}
pathlib.Path(out).write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result))
PY
  [[ $rc -eq 0 ]] || log "WARN forge $id rc=$rc"
}

log "start out=$OUT fuzz=$FUZZ lite=$FUZZ_LITE offline=$FOUNDRY_OFFLINE"

clone_repo "https://github.com/arcadia-finance/accounts-v2.git" "/tmp/arcadia-accounts-v2"
clone_repo "https://github.com/arcadia-finance/lending-v2.git" "/tmp/arcadia-lending-v2"
clone_repo "https://github.com/arcadia-finance/asset-managers.git" "/tmp/arcadia-asset-managers"
clone_repo "https://github.com/silo-finance/silo-contracts-v2.git" "/tmp/silo-contracts-v2" "--recurse-submodules"

set +e
run_forge arcadia_lending_fuzz /tmp/arcadia-lending-v2 'test/fuzz/**/*.fuzz.t.sol' "$FUZZ" "$SOLC34"
# accounts-v2 is multi-solc (0.7.6 + 0.8.26 + 0.8.34) — let forge pick; timeout bounds compile hang
run_forge arcadia_accounts /tmp/arcadia-accounts-v2 'test/fuzz/**/*.fuzz.t.sol' "$FUZZ_LITE" ""
run_forge arcadia_asset_managers /tmp/arcadia-asset-managers 'test/fuzz/**/*.fuzz.t.sol' "$FUZZ_LITE" ""
run_forge silo_v2 /tmp/silo-contracts-v2 'test/**/*.t.sol' 2048 ""

python3 - "$OUT" <<'PY'
import json, pathlib, sys, time
out = pathlib.Path(sys.argv[1])
repos = {}
failures = []
for p in sorted(out.glob("*.json")):
    if p.name == "rollup.json":
        continue
    r = json.loads(p.read_text())
    repos[r["id"]] = r
    if r.get("failed", 0) or r.get("compile_err") or not r.get("ok"):
        failures.append(r["id"])
rollup = {
    "stamp": out.name,
    "time_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "platform": "hackenproof_lowtier",
    "targets": ["arcadia_accounts", "arcadia_lending", "arcadia_asset_managers", "silo_v2"],
    "foundry": repos,
    "failures": failures,
    "verdict": "REVIEW_FAILS" if failures else "NO_BOUNTY_FINDING",
    "write_to_platforms": bool([r for r in repos.values() if r.get("failed", 0) > 0]),
    "note": "Medium/Low on HackenProof Arcadia: $2k/$0 — submit only with Foundry PoC + live impact",
}
(out / "rollup.json").write_text(json.dumps(rollup, indent=2) + "\n")
print(json.dumps(rollup, indent=2))
PY

ln -sfn "$OUT" "$ROOT/reports/bounty/CURRENT_LOWTIER"
log "done → $OUT/rollup.json"
