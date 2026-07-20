#!/usr/bin/env bash
# Publish libheif OSS CVE Watch day: HTML + hub + deploy (+ news when ready).
#
#   DAY=1 OUT=reports/oss-cve-watch-libheif/day01-libfuzzer-STAMP bash scripts/ops/publish_oss_cve_watch_libheif_day_finish.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

DAY="${DAY:?set DAY=N}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
GIT_PUSH="${GIT_PUSH:-1}"
DATE_LOCAL="${DATE_LOCAL:-$(date +%Y-%m-%d)}"
WATCH_DIR="$ROOT/reports/oss-cve-watch-libheif"

log() { echo "[libheif-publish-d$(printf '%02d' "$DAY")] $*"; }

if [[ -z "${OUT:-}" ]]; then
  OUT="$(find "$WATCH_DIR" -maxdepth 1 -type d -name "day$(printf '%02d' "$DAY")-libfuzzer-*" \
    | while read -r d; do
        [[ -f "$d/ROLLUP.json" && -f "$d/SESSION.json" ]] || continue
        echo "$(stat -c '%Y' "$d/ROLLUP.json") $d"
      done | sort -nr | head -1 | cut -d' ' -f2-)"
fi
[[ -n "$OUT" && -f "$OUT/ROLLUP.json" ]] || { log "ERROR: no ROLLUP for day $DAY"; exit 2; }

MIN_ITERATIONS="${MIN_ITERATIONS:-20000000}"
MIN_ELAPSED_SEC="${MIN_ELAPSED_SEC:-82800}"
set +e
GATE_MSG="$(python3 - "$OUT" "$MIN_ITERATIONS" "$MIN_ELAPSED_SEC" <<'PY'
import json, sys
from pathlib import Path
out, min_it, min_el = Path(sys.argv[1]), int(sys.argv[2]), float(sys.argv[3])
s = json.loads((out / "SESSION.json").read_text()) if (out / "SESSION.json").is_file() else {}
r = json.loads((out / "ROLLUP.json").read_text())
iters = int(s.get("iterations") or (r.get("targets") or [{}])[0].get("iterations") or 0)
elapsed = float(s.get("elapsed_sec") or (r.get("targets") or [{}])[0].get("elapsed_sec") or 0)
corp = int(s.get("corpus_count") or 0)
cov = int(s.get("coverage_edges") or 0)
print(f"gate iters={iters} elapsed={elapsed:.1f}s corp={corp} cov={cov} need>={min_it} & >={min_el}s")
ok = iters >= min_it and elapsed >= min_el and corp > 0 and cov > 0
sys.exit(0 if ok else 1)
PY
)"
GATE_RC=$?
set -e
log "$GATE_MSG"
[[ "$GATE_RC" -eq 0 ]] || { log "REFUSE publish — below gate"; exit 4; }

python3 "$ROOT/scripts/ops/export_oss_cve_watch_libheif_libfuzzer.py" "$DAY" "$OUT"
log "exported web/site/reports/oss-cve-watch-libheif/day$(printf '%02d' "$DAY").html"

python3 - <<PY
import json
from pathlib import Path
day = int("$DAY")
out = Path("$OUT")
session = json.loads((out / "SESSION.json").read_text())
rollup = json.loads((out / "ROLLUP.json").read_text())
verdict = rollup.get("verdict") or session.get("verdict") or "CLEAN"
iters = int(session.get("iterations") or 0)
eps = float(session.get("exec_per_sec") or 0)
elapsed = float(session.get("elapsed_sec") or 0)
hours = elapsed / 3600 if elapsed else 0
corp = int(session.get("corpus_count") or 0)
cov = int(session.get("coverage_edges") or 0)
asan = int(session.get("asan_crashes") or 0)
iters_m = iters / 1e6
meta_path = Path("web/site/reports/oss-cve-watch-libheif/meta.json")
cum = iters
if meta_path.is_file():
    meta = json.loads(meta_path.read_text())
    cum = sum(int(d.get("iterations") or 0) for d in meta.get("days") or [] if int(d.get("day") or 0) <= day)
cum_m = cum / 1e6
nid = f"{'$DATE_LOCAL'}-oss-cve-watch-libheif-day{day:02d}-libfuzzer"
item = {
    "id": nid,
    "date": "$DATE_LOCAL",
    "title": f"OSS CVE Watch · libheif Day {day}/14 · {iters_m:.2f}M libFuzzer exec · {verdict}",
    "summary": (
        f"libheif Day {day}: {hours:.1f}h libFuzzer, {iters:,} exec at ~{eps:,.0f}/s. "
        f"Corpus {corp}, {cov} coverage edges. ASAN={asan} — {verdict}."
    ),
    "impact": f"Cumulative Days 1–{day}: ~{cum_m:.2f}M exec on persistent HEIF/AVIF corpus.",
    "action": f"Day {day}: https://hackme.tech/reports/oss-cve-watch-libheif/day{day:02d}.html",
    "tags": ["research", "oss-cve", "fuzzing", "libheif", f"day{day:02d}"],
    "status": "published",
}
for name in ("web/site/assets/news.json",):
    p = Path(name)
    if not p.is_file():
        continue
    data = json.loads(p.read_text())
    items = [x for x in (data.get("items") or []) if x.get("id") != nid]
    data["items"] = [item] + items
    p.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\\n")
    print(f"news upsert {nid}")
print(f"NEWS_ID={nid}")
PY

NODE_SSH="$NODE_SSH" NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}" SKIP_DIST=1 \
  bash "$ROOT/scripts/ops/deploy_hackme_site.sh"

if [[ "$GIT_PUSH" == "1" ]]; then
  git add web/site/reports/oss-cve-watch-libheif/ web/site/assets/news.json web/site/assets/news-display*.json web/site/assets/news-display-index.json web/site/assets/news-chunks/ 2>/dev/null || true
  git add web/site/reports/oss-cve-watch-libheif/ web/site/assets/news.json 2>/dev/null || true
  if ! git diff --cached --quiet; then
    GIT_AUTHOR_NAME='jokeez' GIT_AUTHOR_EMAIL='dney777666@gmail.com' \
    GIT_COMMITTER_NAME='jokeez' GIT_COMMITTER_EMAIL='dney777666@gmail.com' \
      git commit -m "Publish OSS CVE Watch libheif Day $(printf '%02d' "$DAY") ledger."
    git push origin HEAD || true
  fi
fi
log "DONE day $DAY libheif"
