#!/usr/bin/env bash
# libFuzzer session for OSS CVE depth (corpus persists between runs).
#
#   bash scripts/ops/run_oss_libfuzzer_session.sh
#   MAX_TIME=3600 bash scripts/ops/run_oss_libfuzzer_session.sh
#   setsid bash scripts/ops/run_oss_libfuzzer_session.sh >>logs/nghttp2-libfuzzer.nohup.log 2>&1 &
#
# Env:
#   TARGET=nghttp2
#   MAX_TIME=28800     — seconds (default 8h)
#   CORPUS_ROOT=       — default reports/oss-cve-libfuzzer/nghttp2
set -euo pipefail
export PATH="/home/kapa/.local/bin:$HOME/.cargo/bin:$PATH"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

TARGET="${TARGET:-nghttp2}"
MAX_TIME="${MAX_TIME:-28800}"
TIMEOUT="${TIMEOUT:-3}"
RSS_MB="${RSS_LIMIT_MB:-4096}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"

CORPUS_ROOT="${CORPUS_ROOT:-$ROOT/reports/oss-cve-libfuzzer/$TARGET}"
SEEDS="$ROOT/tasks/seeds/oss-libfuzzer/$TARGET"
WORK_CORPUS="$CORPUS_ROOT/corpus"
CRASH_DIR="$CORPUS_ROOT/crashes"
SESSION="$CORPUS_ROOT/sessions/$STAMP"
mkdir -p "$WORK_CORPUS" "$CRASH_DIR" "$SESSION" "$ROOT/logs"

log() { echo "[libfuzzer $(date -u +%H:%M:%S)] $*" | tee -a "$SESSION/run.log"; }

log "=== session start target=$TARGET stamp=$STAMP max_time=${MAX_TIME}s ==="
log "corpus=$WORK_CORPUS (persists across sessions)"

BIN="$(TARGET="$TARGET" bash "$ROOT/scripts/ops/build_oss_libfuzzer.sh" 2>>"$SESSION/build.log" | tail -1)"
[[ -x "$BIN" ]] || { log "build failed"; exit 2; }
log "binary=$BIN"

# Seed once if corpus empty
if [[ -z "$(find "$WORK_CORPUS" -type f 2>/dev/null | head -1)" ]] && [[ -d "$SEEDS" ]]; then
  log "bootstrap corpus from $SEEDS"
  cp -n "$SEEDS"/* "$WORK_CORPUS/" 2>/dev/null || cp "$SEEDS"/* "$WORK_CORPUS/"
fi

export ASAN_OPTIONS="${ASAN_OPTIONS:-detect_leaks=0:halt_on_error=1:allocator_may_return_null=1:print_stacktrace=1}"
export UBSAN_OPTIONS="${UBSAN_OPTIONS:-halt_on_error=1:print_stacktrace=1}"

FUZZ_LOG="$SESSION/fuzzer.log"
log "fuzzing — tail: tail -f $FUZZ_LOG"

set +e
"$BIN" "$WORK_CORPUS" \
  -max_total_time="$MAX_TIME" \
  -timeout="$TIMEOUT" \
  -rss_limit_mb="$RSS_MB" \
  -max_len=65536 \
  -artifact_prefix="$CRASH_DIR/crash-" \
  -print_final_stats=1 \
  2>&1 | tee "$FUZZ_LOG"
RC=${PIPESTATUS[0]}
set -e

python3 "$ROOT/scripts/ops/export_oss_libfuzzer_session.py" "$TARGET" "$SESSION" "$BIN" | tee -a "$SESSION/run.log"

VERDICT="$(python3 -c "import json; print(json.load(open('$SESSION/SESSION.json'))['verdict'])")"
EXEC="$(python3 -c "import json; print(json.load(open('$SESSION/SESSION.json'))['iterations'])")"
log "done rc=$RC verdict=$VERDICT executions=$EXEC"
log "SESSION=$SESSION/SESSION.json"

ln -sfn "$SESSION" "$CORPUS_ROOT/LATEST_SESSION"
ln -sfn "$CORPUS_ROOT" "$ROOT/reports/oss-cve-libfuzzer/CURRENT-$TARGET"

if [[ "$VERDICT" == "CVE_CANDIDATE" ]]; then
  log "*** CVE_CANDIDATE — check $CRASH_DIR and hold disclosure ***"
  exit 1
fi
exit 0
