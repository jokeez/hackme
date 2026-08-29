#!/usr/bin/env bash
# Max Foundry sweep — all open-repo bounty targets (HackenProof + Immunefi OSS).
#
#   bash scripts/ops/run_bounty_open_max.sh
#   CUSTOM_FUZZ=16384 UPSTREAM_FUZZ=8192 bash scripts/ops/run_bounty_open_max.sh
set -euo pipefail
export PATH="/home/kapa/.nvm/versions/node/v24.14.1/bin:/home/kapa/.local/bin:$HOME/.foundry/bin:$HOME/.cargo/bin:$PATH"
export FOUNDRY_OFFLINE="${FOUNDRY_OFFLINE:-true}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/bounty/open-max-${STAMP}}"
REPO_CACHE="${REPO_CACHE:-$ROOT/.cache/bounty-repos}"
LAB="$ROOT/bounty-lab"
CUSTOM_FUZZ="${CUSTOM_FUZZ:-16384}"
UPSTREAM_FUZZ="${UPSTREAM_FUZZ:-8192}"
KLEIDI_FUZZ="${KLEIDI_FUZZ:-16384}"
SOLC34="${SOLC34:-/home/kapa/.local/bin/solc-0.8.34}"

mkdir -p "$OUT"
log() { echo "[open-max $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/sweep.log"; }

ensure_solc() {
  local ver="$1"
  local svm="$HOME/.svm/$ver/solc-$ver"
  [[ -x "$svm" ]] && return 0
  mkdir -p "$HOME/.svm/$ver"
  [[ -x "$HOME/.local/bin/solc-$ver" ]] && ln -sf "$HOME/.local/bin/solc-$ver" "$svm"
}

clone() {
  local url="$1" dest="$2" extra="${3:-}"
  [[ -d "$dest/.git" ]] && return 0
  log "clone $url"
  mkdir -p "$(dirname "$dest")"
  git clone --depth 1 $extra "$url" "$dest" >>"$OUT/clone.log" 2>&1 || true
}

summarize_forge() {
  local id="$1" logf="$2" rc="$3" outjson="$4"
  python3 - "$logf" "$id" "$outjson" "$rc" <<'PY'
import json, pathlib, re, sys
logf, rid, out, rc = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
text = pathlib.Path(logf).read_text(errors="replace") if pathlib.Path(logf).exists() else ""
m = re.search(r"(\d+) tests? passed,\s*(\d+) failed", text)
passed = int(m.group(1)) if m else 0
failed = int(m.group(2)) if m else 0
compile_err = any(x in text for x in ("Compiler run failed", "Unable to resolve imports", "Nothing to compile"))
assume_noise = len(re.findall(r"vm\.assume.*rejected too many", text))
rpc_noise = len(re.findall(r"HTTP error 429|Max retries exceeded", text))
setup_noise = len(re.findall(r"\[FAIL.*setUp\(\)", text))
real_fails = [ln for ln in text.splitlines() if "[FAIL" in ln
              and "setUp()" not in ln and "429" not in ln
              and "rejected too many inputs" not in ln
              and "reverted as expected" not in ln][:15]
result = {
    "id": rid,
    "passed": passed,
    "failed": failed,
    "exit_code": rc,
    "compile_err": compile_err,
    "assume_noise": assume_noise,
    "rpc_noise": rpc_noise,
    "setup_noise": setup_noise,
    "real_fails": real_fails,
    "ok": failed == 0 and not compile_err and passed > 0,
    "bounty_candidate": bool(real_fails) and not compile_err,
}
pathlib.Path(out).write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result))
PY
}

run_forge() {
  local id="$1" dir="$2" args="$3"
  local logf="$OUT/${id}.log"
  log "forge $id $args"
  set +e
  (cd "$dir" && eval "forge test $args" 2>&1 | tee "$logf")
  local rc=$?
  set -e
  summarize_forge "$id" "$logf" "$rc" "$OUT/${id}.json"
}

log "=== open-max sweep CUSTOM=$CUSTOM_FUZZ UPSTREAM=$UPSTREAM_FUZZ ==="

# --- tokenize-it (HackenProof, pinned audit commit) ---
TOKENIZE_PIN="${TOKENIZE_PIN:-52b0322fb566c7143d09c23b7bd30f2e092e0691}"
clone "https://github.com/corpus-io/tokenize.it-smart-contracts.git" "$REPO_CACHE/tokenize-it"
ensure_solc 0.8.34
if [[ -d "$REPO_CACHE/tokenize-it/.git" ]]; then
  (cd "$REPO_CACHE/tokenize-it" && git fetch --depth 1 origin "$TOKENIZE_PIN" 2>/dev/null || true
    git checkout -f "$TOKENIZE_PIN" 2>/dev/null || git checkout -f "${TOKENIZE_PIN:0:7}")
  if [[ ! -d "$REPO_CACHE/tokenize-it/lib/forge-std" ]]; then
    (cd "$REPO_CACHE/tokenize-it" && forge install foundry-rs/forge-std --no-commit) >>"$OUT/clone.log" 2>&1 || true
  fi
  if [[ ! -d "$REPO_CACHE/tokenize-it/node_modules/@openzeppelin" ]]; then
    (cd "$REPO_CACHE/tokenize-it" && npm install --legacy-peer-deps) >>"$OUT/clone.log" 2>&1 || true
  fi
  mkdir -p "$REPO_CACHE/tokenize-it/test/hackme"
  cp -f "$LAB/tokenize-it/HackMe_CoinvestedUltra.t.sol" "$LAB/tokenize-it/HackMe_ExitUltra.t.sol" \
    "$REPO_CACHE/tokenize-it/test/hackme/"
  run_forge tokenize_hackme "$REPO_CACHE/tokenize-it" \
    "--match-contract 'HackMe_CoinvestedUltra|HackMe_ExitUltra' --fuzz-runs $CUSTOM_FUZZ -vv"
  run_forge tokenize_upstream "$REPO_CACHE/tokenize-it" \
    "--match-contract 'CoinvestedPosition|Exit|GlobalTokenExitRegistry|Distribution|Crowdinvesting|PrivateOffer' --no-match-test Mainnet --fuzz-runs $UPSTREAM_FUZZ"
fi

# --- arcadia (HackenProof, public) ---
clone "https://github.com/arcadia-finance/lending-v2.git" "$REPO_CACHE/arcadia-lending-v2"
clone "https://github.com/arcadia-finance/accounts-v2.git" "$REPO_CACHE/arcadia-accounts-v2"
if [[ -d "$REPO_CACHE/arcadia-lending-v2/.git" ]]; then
  mkdir -p "$REPO_CACHE/arcadia-lending-v2/test/hackme"
  cp -f "$LAB/arcadia-lending/"*.sol "$REPO_CACHE/arcadia-lending-v2/test/hackme/"
  run_forge arcadia_hackme "$REPO_CACHE/arcadia-lending-v2" \
    "--match-path 'test/hackme/*.sol' --fuzz-runs $CUSTOM_FUZZ -vv"
  run_forge arcadia_lending_fuzz "$REPO_CACHE/arcadia-lending-v2" \
    "--use '$SOLC34' --match-path 'test/fuzz/**/*.fuzz.t.sol' --fuzz-runs $UPSTREAM_FUZZ"
fi

# --- kleidi (Immunefi, public) ---
clone "https://github.com/solidity-labs-io/kleidi.git" "$REPO_CACHE/kleidi"
ensure_solc 0.8.25
if [[ -d "$REPO_CACHE/kleidi/.git" ]]; then
  [[ -d "$REPO_CACHE/kleidi/lib/forge-std" ]] || \
    (cd "$REPO_CACHE/kleidi" && forge install foundry-rs/forge-std --no-commit) >>"$OUT/clone.log" 2>&1 || true
  run_forge kleidi_unit "$REPO_CACHE/kleidi" \
    "--match-path 'test/unit/*.t.sol' --fuzz-runs $KLEIDI_FUZZ"
fi

# --- 0xmarkets (HackenProof, public recon — often missing deps) ---
clone "https://github.com/taoshidev/0xmarkets_contract.git" "$REPO_CACHE/0xmarkets_contract"
if [[ -d "$REPO_CACHE/0xmarkets_contract/contracts" ]]; then
  run_forge oxmarkets "$REPO_CACHE/0xmarkets_contract" \
    "--fuzz-runs 2048" || true
fi

python3 - "$OUT" <<'PY'
import json, pathlib, sys, time

out = pathlib.Path(sys.argv[1])
targets = {}
candidates = []
for p in sorted(out.glob("*.json")):
    if p.name == "rollup.json":
        continue
    r = json.loads(p.read_text())
    targets[r["id"]] = r
    if r.get("bounty_candidate"):
        candidates.append({"id": r["id"], "fails": r.get("real_fails", [])[:5]})

noise_only = all(
    not t.get("bounty_candidate")
    for t in targets.values()
    if not t.get("compile_err")
)

verdict = "BOUNTY_CANDIDATE" if candidates else ("NO_BOUNTY_FINDING" if noise_only else "INFRA_BLOCKED")

rollup = {
    "stamp": out.name,
    "time_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "strategy": "open_repo_max_foundry",
    "targets_open": [
        {"id": "tokenize_it", "platform": "hackenproof", "repo": "corpus-io/tokenize.it-smart-contracts", "max_usd": 5000, "ends": "2026-06-27", "subs": 85},
        {"id": "arcadia", "platform": "hackenproof", "repo": "arcadia-finance/*", "max_usd": 25000, "medium_usd": 2000, "subs": 66},
        {"id": "kleidi", "platform": "immunefi", "repo": "solidity-labs-io/kleidi", "max_usd": 50000},
        {"id": "0xmarkets", "platform": "hackenproof", "repo": "taoshidev/0xmarkets_contract", "max_usd": 30000, "subs": 451, "note": "high competition"},
    ],
    "targets_skipped_closed": [
        {"id": "darts_rwa", "reason": "private repo — need HackenProof invite"},
        {"id": "tiprun", "reason": "critical pays $0, 355 subs"},
    ],
    "foundry": targets,
    "candidates": candidates,
    "verdict": verdict,
    "write_to_platforms": bool(candidates),
    "next_manual": [
        "tokenize-it: manual Exit/Coinvested/AllowList logic review (automated clean)",
        "arcadia: Base mainnet fork + liquidation edge cases",
        "darts: join program + request repo after KYC",
        "kleidi: integration tests need SAFE_FACTORY on fork — review wallet spell logic manually",
    ],
}
(out / "rollup.json").write_text(json.dumps(rollup, indent=2) + "\n")
print(json.dumps(rollup, indent=2))
PY

ln -sfn "$OUT" "$ROOT/reports/bounty/CURRENT_OPEN_MAX"
log "done → $OUT/rollup.json"
