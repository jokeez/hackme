#!/usr/bin/env bash
# Discovery fuzz — clone any public bounty-adjacent repo and run Foundry if present.
#
#   bash scripts/ops/run_bounty_discovery_fuzz.sh
#   FUZZ_RUNS=4096 bash scripts/ops/run_bounty_discovery_fuzz.sh
set -euo pipefail
export PATH="/home/kapa/.nvm/versions/node/v24.14.1/bin:/home/kapa/.local/bin:$HOME/.foundry/bin:$PATH"
export FOUNDRY_OFFLINE="${FOUNDRY_OFFLINE:-true}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/bounty/discovery-${STAMP}}"
CACHE="${CACHE:-$ROOT/.cache/bounty-discovery}"
FUZZ="${FUZZ_RUNS:-2048}"

mkdir -p "$OUT" "$CACHE"
log() { echo "[discovery $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/discovery.log"; }

# id|git_url|forge_args (optional)
TARGETS=(
  "ignite_market|https://github.com/hackenproof-public/ignite-market-smart-contracts.git|--fuzz-runs $FUZZ"
  "commonwealth|https://github.com/hackenproof-public/commonwealth-contracts.git|--fuzz-runs $FUZZ"
  "moonwell_v2|https://github.com/moonwell-fi/moonwell-contracts-v2.git|--fuzz-runs $FUZZ"
  "euler_vault_kit|https://github.com/euler-xyz/euler-vault-kit.git|--fuzz-runs $FUZZ"
  "reserve_protocol|https://github.com/reserve-protocol/protocol.git|--fuzz-runs $FUZZ"
  "morpho_blue|https://github.com/morpho-org/morpho-blue.git|--fuzz-runs $FUZZ"
  "angle_transmuter|https://github.com/AngleProtocol/angle-transmuter.git|--fuzz-runs $FUZZ"
  "silo_v2|https://github.com/silo-finance/silo-contracts-v2.git|--recurse-submodules|--fuzz-runs $FUZZ"
  "badger_vaults|https://github.com/Badger-Finance/badger-vaults.git|--fuzz-runs 512"
  "compound_comet|https://github.com/compound-finance/comet.git|--fuzz-runs 512"
)

clone_repo() {
  local id="$1" url="$2" extra="${3:-}"
  local dest="$CACHE/$id"
  if [[ -d "$dest/.git" ]]; then
    log "skip clone $id"
    return 0
  fi
  log "clone $id"
  rm -rf "$dest"
  git clone --depth 1 $extra "$url" "$dest" >>"$OUT/clone.log" 2>&1 || return 1
}

run_forge() {
  local id="$1" dir="$2" args="$3"
  local logf="$OUT/${id}.log"
  [[ -f "$dir/foundry.toml" ]] || [[ -f "$dir/hardhat.config.ts" ]] || { log "skip $id (no foundry)"; return 0; }
  [[ -f "$dir/foundry.toml" ]] || { log "skip $id (hardhat only)"; return 0; }
  log "forge $id"
  set +e
  (cd "$dir" && forge test $args -vv 2>&1 | tee "$logf")
  local rc=$?
  set -e
  python3 - "$logf" "$id" "$OUT/${id}.json" "$rc" <<'PY'
import json, pathlib, re, sys
logf, rid, out, rc = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
text = pathlib.Path(logf).read_text(errors="replace") if pathlib.Path(logf).exists() else ""
m = re.search(r"(\d+) tests? passed,\s*(\d+) failed", text)
passed = int(m.group(1)) if m else 0
failed = int(m.group(2)) if m else 0
real = [ln for ln in text.splitlines() if "[FAIL" in ln and "setUp()" not in ln
        and "rejected too many" not in ln and "429" not in ln][:12]
pathlib.Path(out).write_text(json.dumps({
    "id": rid, "passed": passed, "failed": failed, "exit_code": rc,
    "compile_err": "Compiler run failed" in text,
    "real_fails": real, "bounty_candidate": bool(real) and failed > 0,
}, indent=2) + "\n")
PY
}

log "start fuzz=$FUZZ targets=${#TARGETS[@]}"
# Also load auto-fetched hackenproof-public repos
DISC_JSON="${ROOT}/upstream/bounty_discovery_repos.json"
if [[ -f "$DISC_JSON" ]]; then
  while IFS= read -r line; do
    [[ -n "$line" ]] && TARGETS+=("$line")
  done < <(python3 - "$DISC_JSON" <<'PY'
import json, sys
for r in json.load(open(sys.argv[1])).get("repos", []):
    url = r.get("url")
    rid = r.get("id")
    if url and rid:
        print(f"{rid}|{url}|--fuzz-runs {__import__('os').environ.get('FUZZ_RUNS','2048')}")
PY
)
fi
for entry in "${TARGETS[@]}"; do
  IFS='|' read -r id url rest <<< "$entry"
  extra=""
  args="--fuzz-runs $FUZZ"
  if [[ "$rest" == --recurse-submodules* ]]; then
    extra="--recurse-submodules"
    args="${rest#--recurse-submodules }"
  else
    args="$rest"
  fi
  clone_repo "$id" "$url" "$extra" || continue
  dest="$CACHE/$id"
  if [[ "$id" == "silo_v2" ]] && [[ -d "$dest" ]]; then
    (cd "$dest" && git submodule update --init --recursive) >>"$OUT/clone.log" 2>&1 || true
  fi
  if [[ -d "$dest/node_modules" ]] || [[ -f "$dest/package.json" ]]; then
    (cd "$dest" && npm install --legacy-peer-deps) >>"$OUT/clone.log" 2>&1 || true
  fi
  if [[ -d "$dest/lib/forge-std" ]] || [[ -f "$dest/foundry.toml" ]]; then
    (cd "$dest" && forge install foundry-rs/forge-std --no-commit) >>"$OUT/clone.log" 2>&1 || true
  fi
  run_forge "$id" "$dest" "$args" || true
done

python3 - "$OUT" <<'PY'
import json, pathlib, sys, time
out = pathlib.Path(sys.argv[1])
repos = {}
candidates = []
for p in sorted(out.glob("*.json")):
    if p.name == "rollup.json": continue
    r = json.loads(p.read_text())
    repos[r["id"]] = r
    if r.get("bounty_candidate"):
        candidates.append(r)
rollup = {
    "stamp": out.name,
    "time_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "strategy": "public_repo_discovery_fuzz",
    "repos": repos,
    "candidates": candidates,
    "verdict": "BOUNTY_CANDIDATE" if candidates else "NO_BOUNTY_FINDING",
}
(out / "rollup.json").write_text(json.dumps(rollup, indent=2) + "\n")
print(json.dumps(rollup, indent=2))
PY

ln -sfn "$OUT" "$ROOT/reports/bounty/CURRENT_DISCOVERY"
log "done → $OUT/rollup.json"
