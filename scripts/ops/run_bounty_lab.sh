#!/usr/bin/env bash
# Bounty lab — low-competition targets + custom invariant/fuzz (not upstream test reruns).
#
#   bash scripts/ops/run_bounty_lab.sh
set -euo pipefail
export PATH="/home/kapa/.nvm/versions/node/v24.14.1/bin:/home/kapa/.local/bin:$HOME/.foundry/bin:$PATH"
export FOUNDRY_OFFLINE="${FOUNDRY_OFFLINE:-true}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/bounty/bounty-lab-${STAMP}}"
FUZZ="${FOUNDRY_FUZZ_RUNS:-4096}"
CUSTOM_FUZZ="${CUSTOM_FUZZ_RUNS:-8192}"
SKIP_UPSTREAM="${SKIP_UPSTREAM:-0}"
LAB="$ROOT/bounty-lab"
REPO_CACHE="${REPO_CACHE:-$ROOT/.cache/bounty-repos}"

mkdir -p "$OUT"
log() { echo "[bounty-lab $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/lab.log"; }

ensure_solc_0823() {
  local svm="$HOME/.svm/0.8.23/solc-0.8.23"
  [[ -x "$svm" ]] && return 0
  mkdir -p "$HOME/.svm/0.8.23"
  ln -sf /home/kapa/.local/bin/solc-0.8.23 "$svm"
}

prepare_tokenize() {
  ensure_solc_0823
  [[ -d "$REPO_CACHE/tokenize-it/.git" ]] || return 0
  if [[ ! -d "$REPO_CACHE/tokenize-it/lib/forge-std" ]]; then
    (cd "$REPO_CACHE/tokenize-it" && forge install foundry-rs/forge-std --no-commit) >>"$OUT/clone.log" 2>&1 || true
  fi
  if [[ ! -d "$REPO_CACHE/tokenize-it/node_modules/@openzeppelin" ]]; then
    log "npm install tokenize-it deps"
    local npm_bin="/home/kapa/.nvm/versions/node/v24.14.1/bin"
    (cd "$REPO_CACHE/tokenize-it" && PATH="$npm_bin:$PATH" npm install --legacy-peer-deps) >>"$OUT/clone.log" 2>&1 || true
  fi
}

prepare_silo() {
  [[ -d "$REPO_CACHE/silo-contracts-v2/.git" ]] || return 0
  (cd "$REPO_CACHE/silo-contracts-v2" && git submodule update --init --recursive) >>"$OUT/clone.log" 2>&1 || true
}

prepare_arcadia() {
  for repo in arcadia-lending-v2 arcadia-accounts-v2; do
    [[ -d "$REPO_CACHE/$repo/.git" ]] || continue
    if [[ ! -d "$REPO_CACHE/$repo/out" ]]; then
      log "arcadia prebuild $repo (first run: submodules)"
      (cd "$REPO_CACHE/$repo" && git submodule update --init --recursive && forge build) \
        >>"$OUT/clone.log" 2>&1 || true
    fi
  done
}

clone() {
  local url="$1" dest="$2" extra="${3:-}"
  mkdir -p "$REPO_CACHE"
  [[ -d "$dest/.git" ]] && return 0
  local name; name="$(basename "$dest")"
  log "clone $url → $dest"
  (cd "$REPO_CACHE" && rm -rf "$name" && git clone --depth 1 $extra "$url" "$name") >>"$OUT/clone.log" 2>&1
}

deploy_arcadia_custom() {
  local dest="$REPO_CACHE/arcadia-lending-v2/test/hackme"
  mkdir -p "$dest"
  cp -f "$LAB/arcadia-lending/"*.sol "$dest/"
  log "deployed custom arcadia tests → $dest"
}

deploy_tokenize_custom() {
  local dest="$REPO_CACHE/tokenize-it/test/hackme"
  mkdir -p "$dest"
  cp -f "$LAB/tokenize-it/"*.sol "$dest/"
  log "deployed custom tokenize tests → $dest"
}

run_forge_summary() {
  local id="$1" dir="$2" args="$3"
  local logf="$OUT/${id}.log"
  log "forge $id $args"
  set +e
  (cd "$dir" && eval "forge test $args" 2>&1 | grep -vE '^(Receiving objects|Resolving deltas|remote:|Cloning into|Submodule |From https)' | tee "$logf")
  local rc=$?
  set -e
  python3 - "$logf" "$id" "$OUT/${id}.json" "$rc" <<'PY'
import json, pathlib, re, sys
logf, rid, out, rc = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
text = pathlib.Path(logf).read_text(errors="replace") if pathlib.Path(logf).exists() else ""
m = re.search(r"(\d+) tests? passed,\s*(\d+) failed", text)
passed = int(m.group(1)) if m else 0
failed = int(m.group(2)) if m else 0
fail_lines = [ln for ln in text.splitlines() if "[FAIL" in ln and "testFuzz" in ln][:30]
real_fails = [ln for ln in fail_lines if "reverted as expected, but without data" not in ln and "setUp()" not in ln
              and "panic: arithmetic underflow or overflow" not in ln]
result = {"id": rid, "passed": passed, "failed": failed, "exit_code": rc,
          "harness_noise": len(fail_lines) - len(real_fails),
          "interesting_fails": real_fails[:10], "ok": failed == 0 and passed > 0}
pathlib.Path(out).write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result))
PY
}

log "start out=$OUT fuzz=$FUZZ"

# --- Targets (low subs / live audit) ---
clone "https://github.com/corpus-io/tokenize.it-smart-contracts.git" "$REPO_CACHE/tokenize-it"
prepare_tokenize
clone "https://github.com/arcadia-finance/lending-v2.git" "$REPO_CACHE/arcadia-lending-v2"
clone "https://github.com/arcadia-finance/accounts-v2.git" "$REPO_CACHE/arcadia-accounts-v2"
clone "https://github.com/silo-finance/silo-contracts-v2.git" "$REPO_CACHE/silo-contracts-v2" "--recurse-submodules"
prepare_silo

deploy_arcadia_custom
deploy_tokenize_custom

prepare_arcadia

run_forge_summary arcadia_custom "$REPO_CACHE/arcadia-lending-v2" \
  "--match-path 'test/hackme/*.sol' --fuzz-runs $CUSTOM_FUZZ -vv"

run_forge_summary tokenize_custom "$REPO_CACHE/tokenize-it" \
  "--match-path 'test/hackme/*.sol' --fuzz-runs $CUSTOM_FUZZ"

if [[ "$SKIP_UPSTREAM" != "1" ]]; then
  run_forge_summary tokenize_it "$REPO_CACHE/tokenize-it" \
    "--no-match-test Mainnet --no-match-path 'test/hackme/*' --fuzz-runs $FUZZ"
else
  log "skip tokenize_it upstream (SKIP_UPSTREAM=1)"
  run_forge_summary tokenize_blind "$REPO_CACHE/tokenize-it" \
    "--match-path 'test/VestingBlind.t.sol' --fuzz-runs $CUSTOM_FUZZ"
  run_forge_summary tokenize_distribution "$REPO_CACHE/tokenize-it" \
    "--match-path 'test/DistributionCloneFactory.t.sol' --fuzz-runs $CUSTOM_FUZZ"
  run_forge_summary tokenize_timelock "$REPO_CACHE/tokenize-it" \
    "--match-path 'test/TimeLockDistributeExit.t.sol' --fuzz-runs $CUSTOM_FUZZ"
  run_forge_summary tokenize_coinvested "$REPO_CACHE/tokenize-it" \
    "--match-path 'test/CoinvestedPosition*.t.sol' --fuzz-runs 2048"
fi

run_forge_summary arcadia_lending_deep "$REPO_CACHE/arcadia-lending-v2" \
  "--match-path 'test/fuzz/liquidators/**/*.fuzz.t.sol' --fuzz-runs $FUZZ"

if [[ -d "$REPO_CACHE/arcadia-accounts-v2/.git" ]]; then
  log "arcadia accounts build"
  (cd "$REPO_CACHE/arcadia-accounts-v2" && forge build) >>"$OUT/arcadia_accounts_build.log" 2>&1 || true
fi
run_forge_summary arcadia_accounts "$REPO_CACHE/arcadia-accounts-v2" \
  "--match-path 'test/fuzz/accounts/**/*.fuzz.t.sol' --fuzz-runs $FUZZ"

if [[ -d "$REPO_CACHE/silo-contracts-v2/silo-core" ]]; then
  log "silo build (core_with_test)"
  (cd "$REPO_CACHE/silo-contracts-v2" && FOUNDRY_PROFILE=core_with_test forge build) >>"$OUT/silo_build.log" 2>&1 || true
  run_forge_summary silo_v2 "$REPO_CACHE/silo-contracts-v2" \
    "FOUNDRY_PROFILE=core_with_test forge test --fuzz-runs 2048" || true
fi

python3 - "$OUT" <<'PY'
import json, pathlib, sys, time
out = pathlib.Path(sys.argv[1])
repos = {}
interesting = []
for p in sorted(out.glob("*.json")):
    if p.name == "rollup.json": continue
    r = json.loads(p.read_text())
    repos[r["id"]] = r
    if r.get("interesting_fails"):
        interesting.append({"id": r["id"], "fails": r["interesting_fails"]})
verdict = "REVIEW_INTERESTING" if interesting else "NO_BOUNTY_FINDING"
rollup = {
    "stamp": out.name, "time_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "strategy": "custom_invariants + low_competition_audit_targets",
    "targets": list(repos.keys()),
    "foundry": repos,
    "interesting": interesting,
    "verdict": verdict,
    "write_to_platforms": bool(interesting),
    "priority_programs": [
        {"id": "darts_rwa", "subs": 18, "max_usd": 3000, "note": "repo private on hackenproof"},
        {"id": "tokenize_it", "subs": 76, "max_usd": 5000, "ends": "2026-06-27"},
        {"id": "arcadia_hackenproof", "subs": 61, "max_usd": 2000, "medium": True},
    ],
}
(out / "rollup.json").write_text(json.dumps(rollup, indent=2) + "\n")
print(json.dumps(rollup, indent=2))
PY

ln -sfn "$OUT" "$ROOT/reports/bounty/CURRENT_LAB"
log "done → $OUT/rollup.json"
