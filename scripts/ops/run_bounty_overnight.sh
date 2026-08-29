#!/usr/bin/env bash
# Overnight multi-project bounty marathon — WASM + native + Foundry loops until duration ends.
#
#   bash scripts/ops/run_bounty_overnight.sh
#   DURATION_HOURS=8 BUDGET_RUNS=512 FOUNDRY_FUZZ=2048 bash scripts/ops/run_bounty_overnight.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export PATH="$HOME/.foundry/bin:$HOME/.cargo/bin:$PATH"

STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
RUN_ID="${RUN_ID:-bounty-overnight-${STAMP}}"
HUNT_OUT="${HUNT_OUT:-$ROOT/reports/bounty/overnight/$RUN_ID}"
QUEUE="${QUEUE:-$ROOT/upstream/bounty_overnight_queue.json}"

DURATION_HOURS="${DURATION_HOURS:-8}"
BUDGET_RUNS="${BUDGET_RUNS:-512}"
BUDGET_HMC="${BUDGET_HMC:-0.5}"
CHECK_SEMANTICS="${CHECK_SEMANTICS:-detector}"
FOUNDRY_FUZZ_RUNS="${FOUNDRY_FUZZ_RUNS:-2048}"
WASM_SLEEP_SEC="${WASM_SLEEP_SEC:-3}"
SKIP_RUST="${SKIP_RUST:-1}"
SOLC="${SOLC:-/home/kapa/.local/bin/solc-0.8.25}"

END_EPOCH="$(python3 -c "import time; print(int(time.time()) + int(float('${DURATION_HOURS}') * 3600))")"

mkdir -p "$HUNT_OUT"/{wasm,foundry,native,rust,checkpoints}
ln -sfn "$HUNT_OUT" "$ROOT/reports/bounty/overnight/CURRENT"

log() { echo "[bounty-overnight $(date -u +%H:%M:%S)] $*" | tee -a "$HUNT_OUT/marathon.log"; }

log "RUN_ID=$RUN_ID duration=${DURATION_HOURS}h runs=$BUDGET_RUNS fuzz=$FOUNDRY_FUZZ_RUNS end_epoch=$END_EPOCH"

# --- Preflight (retry on SQLITE_BUSY) ---
PF_OUT="$HUNT_OUT/preflight"
PF_OK=0
for attempt in 1 2 3; do
  if OUT="$PF_OUT" bash "$ROOT/scripts/ops/bounty_overnight_preflight.sh" >>"$HUNT_OUT/marathon.log" 2>&1; then
    PF_OK=1
    break
  fi
  log "preflight attempt $attempt failed — sleep 15s"
  sleep 15
done
[[ "$PF_OK" == "1" ]] || { log "preflight failed after 3 attempts — abort"; exit 2; }
cp "$PF_OUT/result.json" "$HUNT_OUT/preflight.json"

# --- Clone repos ---
clone_repo() {
  local url="$1" dest="$2"
  if [[ -d "$dest/.git" ]]; then
    log "clone skip (exists) $dest"
    return 0
  fi
  log "clone $url → $dest"
  git clone --depth 1 "$url" "$dest" >>"$HUNT_OUT/clone.log" 2>&1 || log "WARN clone failed $dest"
}

python3 - "$QUEUE" <<'PY' | while IFS=$'\t' read -r url path; do
import json, sys
q = json.loads(open(sys.argv[1]).read())
seen = set()
for section in ("clone_only", "foundry_repos", "rust_jobs"):
    for item in q.get(section, []):
        url, path = item.get("clone"), item.get("path")
        if url and path and path not in seen:
            seen.add(path)
            print(f"{url}\t{path}")
PY
  [[ -n "${url:-}" ]] && clone_repo "$url" "$path"
done

bash "$ROOT/scripts/build_immunefi_pack.sh" >>"$HUNT_OUT/build.log" 2>&1

readarray -t WASM_TARGETS < <(python3 -c "
import json
for t in json.load(open('$QUEUE'))['wasm_all']:
    print(t)
")

# --- Foundry runner ---
run_foundry_repo() {
  local id="$1" path="$2" match="$3" fuzz="$4"
  local out="$HUNT_OUT/foundry/$id"
  mkdir -p "$out"
  [[ -d "$path" ]] || { log "foundry skip $id (no repo)"; return 0; }
  [[ -x "$SOLC" ]] || { log "foundry skip $id (no solc)"; return 0; }
  log "foundry $id fuzz=$fuzz"
  (
    cd "$path"
    forge test --use "$SOLC" --match-path "$match" --fuzz-runs "$fuzz" -vv \
      2>&1 | tee "$out/run.log" | tail -15
  ) >>"$HUNT_OUT/marathon.log" 2>&1 || log "WARN foundry $id failed"
  python3 - "$out/run.log" "$id" "$out/result.json" <<'PY'
import json, pathlib, re, sys
log, rid, out = pathlib.Path(sys.argv[1]), sys.argv[2], pathlib.Path(sys.argv[3])
text = log.read_text(errors="replace") if log.exists() else ""
passed = failed = 0
m = re.search(r"(\d+) tests? passed,\s*(\d+) failed", text)
if m:
    passed, failed = int(m.group(1)), int(m.group(2))
elif "Suite result: ok" in text:
    passed = 1
result = {"id": rid, "passed": passed, "failed": failed, "ok": failed == 0 and passed > 0}
out.write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result))
PY
}

# --- Native ---
run_native() {
  local id="$1" script="$2" rounds="${3:-}"
  local out="$HUNT_OUT/native/${id}-round${ROUND}"
  mkdir -p "$out"
  log "native $id rounds=${rounds:-default}"
  if [[ -n "$rounds" ]]; then
    OUT="$out" ROUNDS="$rounds" bash "$ROOT/$script" >>"$HUNT_OUT/marathon.log" 2>&1 || log "WARN native $id"
  else
    bash "$ROOT/$script" >>"$HUNT_OUT/marathon.log" 2>&1 || log "WARN native $id"
  fi
  for f in "$out"/summary.json "$HUNT_OUT"/native-wormhole/summary.json; do
    [[ -f "$f" ]] && cp "$f" "$HUNT_OUT/native/${id}-latest.json" 2>/dev/null || true
  done
}

# --- WASM single target ---
run_wasm() {
  local t="$1"
  local out="$HUNT_OUT/wasm/${t}/round-${ROUND}"
  mkdir -p "$out"
  log "wasm round=$ROUND $t runs=$BUDGET_RUNS"
  OUT="$out" TARGET="$t" STAMP="${RUN_ID}-r${ROUND}-${t}" \
    BUDGET_RUNS="$BUDGET_RUNS" BUDGET_HMC="$BUDGET_HMC" CHECK_SEMANTICS="$CHECK_SEMANTICS" \
    bash "$ROOT/scripts/ops/run_immunefi_pilot.sh" >>"$HUNT_OUT/marathon.log" 2>&1 \
    || log "WARN wasm $t round $ROUND"
  if [[ -f "$out/summary.json" ]]; then
    jq -c --argjson r "$ROUND" '. + {round: $r}' "$out/summary.json" >>"$HUNT_OUT/wasm_all.jsonl"
    ln -sf "$out/summary.json" "$HUNT_OUT/wasm/${t}/latest.json"
  fi
  sleep "$WASM_SLEEP_SEC"
}

# --- Rust (optional, once per run) ---
run_rust_once() {
  [[ "$SKIP_RUST" == "1" ]] && return 0
  python3 - "$QUEUE" "$HUNT_OUT" <<'PY'
import json, pathlib, subprocess, sys
q, hunt = json.loads(open(sys.argv[1]).read()), pathlib.Path(sys.argv[2])
for job in q.get("rust_jobs", []):
    if job.get("optional"):
        path = job["path"]
        if not pathlib.Path(path).exists():
            continue
        out = hunt / "rust" / job["id"]
        out.mkdir(parents=True, exist_ok=True)
        logf = out / "run.log"
        cmd = f"cd {path} && {job['cmd']}"
        print(f"rust {job['id']} timeout={job.get('timeout_sec',3600)}")
        try:
            r = subprocess.run(cmd, shell=True, timeout=int(job.get("timeout_sec", 3600)),
                               stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
            logf.write_text(r.stdout)
            ok = r.returncode == 0
        except subprocess.TimeoutExpired as e:
            logf.write_text((e.stdout or "") + "\nTIMEOUT\n")
            ok = False
        (out / "result.json").write_text(json.dumps({"id": job["id"], "ok": ok}) + "\n")
PY
}

write_checkpoint() {
  python3 - "$HUNT_OUT" "$RUN_ID" "$ROUND" "$END_EPOCH" <<'PY'
import json, pathlib, sys, time
hunt, run_id, rnd, end_ep = pathlib.Path(sys.argv[1]), sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
wasm = []
jp = hunt / "wasm_all.jsonl"
if jp.exists():
    wasm = [json.loads(l) for l in jp.read_text().splitlines() if l.strip()]
foundry = []
for p in sorted((hunt / "foundry").glob("*/result.json")):
    foundry.append(json.loads(p.read_text()))
crit = sum(r.get("critical_count", 0) for r in wasm)
guards = sum(r.get("guard_signal_count", 0) for r in wasm)
ffail = [f for f in foundry if not f.get("ok")]
native_panics = 0
for p in hunt.glob("native/*-latest.json"):
    native_panics += json.loads(p.read_text()).get("panics", 0)
verdict = "NO_BOUNTY_FINDING"
write = False
if native_panics > 0 or crit > 0:
    verdict = "CANDIDATE_REVIEW"
    write = True
elif any(f.get("failed", 0) > 0 for f in foundry):
    verdict = "FOUNDRY_FAILURE_REVIEW"
rollup = {
    "run_id": run_id,
    "round": rnd,
    "time_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "seconds_remaining": max(0, end_ep - int(time.time())),
    "wasm_campaigns": len(wasm),
    "wasm_critical": crit,
    "wasm_guard_signals": guards,
    "foundry_jobs": foundry,
    "foundry_failures": ffail,
    "native_panics": native_panics,
    "verdict": verdict,
    "write_to_platforms": write,
}
(hunt / "rollup.json").write_text(json.dumps(rollup, indent=2) + "\n")
(hunt / "checkpoints" / f"round-{rnd:04d}.json").write_text(json.dumps(rollup, indent=2) + "\n")
print(json.dumps({k: rollup[k] for k in ("round", "wasm_campaigns", "wasm_critical", "verdict", "seconds_remaining")}))
PY
}

# --- Main loop ---
ROUND=0
run_rust_once &
RUST_PID=$!

while [[ "$(date +%s)" -lt "$END_EPOCH" ]]; do
  ROUND=$((ROUND + 1))
  log "========== ROUND $ROUND =========="

  run_native wormhole_unmarshal scripts/ops/immunefi_native_wormhole.sh 500000
  run_native wormhole_verify scripts/ops/immunefi_native_wormhole_verify.sh ""

  while IFS=$'\t' read -r id path match fuzz priority; do
    [[ "$(date +%s)" -ge "$END_EPOCH" ]] && break
    run_foundry_repo "$id" "$path" "$match" "$fuzz"
  done < <(python3 - "$QUEUE" "$FOUNDRY_FUZZ_RUNS" <<'PY'
import json, sys
q, default_fuzz = json.loads(open(sys.argv[1]).read()), int(sys.argv[2])
for item in sorted(q.get("foundry_repos", []), key=lambda x: x.get("priority", 9)):
    fuzz = item.get("fuzz_runs") or default_fuzz
    print(f"{item['id']}\t{item['path']}\t{item.get('test_match','test/**/*.t.sol')}\t{fuzz}\t{item.get('priority',9)}")
PY
)

  for t in "${WASM_TARGETS[@]}"; do
    [[ "$(date +%s)" -ge "$END_EPOCH" ]] && break
    run_wasm "$t"
  done

  write_checkpoint | tee -a "$HUNT_OUT/marathon.log"
done

wait "$RUST_PID" 2>/dev/null || true
write_checkpoint | tee -a "$HUNT_OUT/marathon.log"
log "done → $HUNT_OUT/rollup.json ($(wc -l <"$HUNT_OUT/wasm_all.jsonl" 2>/dev/null || echo 0) wasm campaigns)"
