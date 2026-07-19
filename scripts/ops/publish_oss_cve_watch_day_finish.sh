#!/usr/bin/env bash
# After an OSS CVE Watch libFuzzer day finishes: export HTML, news, research card,
# deploy site, Telegram, verify live URLs, optional git push.
#
#   DAY=9 bash scripts/ops/publish_oss_cve_watch_day_finish.sh
#   DAY=9 OUT=reports/oss-cve-watch/day09-libfuzzer-STAMP bash scripts/ops/publish_oss_cve_watch_day_finish.sh
#   DAY=9 GIT_PUSH=1 bash scripts/ops/publish_oss_cve_watch_day_finish.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

DAY="${DAY:?set DAY=N}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
GIT_PUSH="${GIT_PUSH:-1}"
DATE_LOCAL="${DATE_LOCAL:-$(date +%Y-%m-%d)}"

log() { echo "[cve-publish-d$(printf '%02d' "$DAY")] $*"; }

if [[ -z "${OUT:-}" ]]; then
  OUT="$(find "$ROOT/reports/oss-cve-watch" -maxdepth 1 -type d -name "day$(printf '%02d' "$DAY")-libfuzzer-*" \
    | while read -r d; do
        [[ -f "$d/ROLLUP.json" && -f "$d/SESSION.json" ]] || continue
        echo "$(stat -c '%Y' "$d/ROLLUP.json") $d"
      done | sort -nr | head -1 | cut -d' ' -f2-)"
fi
[[ -n "$OUT" && -f "$OUT/ROLLUP.json" ]] || {
  log "ERROR: no ROLLUP for day $DAY (OUT=${OUT:-empty})"
  exit 2
}
log "report=$OUT"

# Refuse premature / stub publishes (empty corpus, seconds-long runs).
MIN_ITERATIONS="${MIN_ITERATIONS:-50000000}"
MIN_ELAPSED_SEC="${MIN_ELAPSED_SEC:-3600}"
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
print(f"gate iters={iters} elapsed={elapsed:.1f}s corp={corp} cov={cov} need>={min_it} iters & >={min_el}s")
ok = iters >= min_it and elapsed >= min_el and corp > 0 and cov > 0
sys.exit(0 if ok else 1)
PY
)"
GATE_RC=$?
set -e
log "$GATE_MSG"
if [[ "$GATE_RC" -ne 0 ]]; then
  log "REFUSE publish: session below MIN_ITERATIONS/MIN_ELAPSED_SEC (not a real depth day)"
  exit 4
fi

python3 "$ROOT/scripts/ops/export_oss_cve_watch_libfuzzer.py" "$DAY" "$OUT"
log "exported web/site/reports/oss-cve-watch/day$(printf '%02d' "$DAY").html"

python3 - <<PY
import json, re
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
corp_kb = int((session.get("corpus_bytes") or 0) / 1024)

# Cumulative days 2..day from meta if present
meta_path = Path("web/site/reports/oss-cve-watch/meta.json")
cum = iters
if meta_path.is_file():
    meta = json.loads(meta_path.read_text())
    cum = 0
    for d in meta.get("days") or []:
        if int(d.get("day") or 0) >= 2 and int(d.get("day") or 0) <= day:
            cum += int(d.get("iterations") or 0)
cum_b = cum / 1e9
iters_b = iters / 1e9
nid = f"{'$DATE_LOCAL'}-oss-cve-watch-day{day:02d}-nghttp2-libfuzzer"

item = {
    "id": nid,
    "date": "$DATE_LOCAL",
    "title": f"OSS CVE Watch Day {day}/14 · nghttp2 · {iters_b:.2f}B libFuzzer exec · {verdict}",
    "summary": (
        f"Day {day} on nghttp2: {hours:.1f}h libFuzzer depth, {iters:,} executions"
        f" at ~{eps:,.0f} exec/s. Corpus {corp} ({corp_kb}KB), {cov} coverage edges."
        f" ASAN heap crashes: {asan} — verdict {verdict}."
    ),
    "impact": (
        f"Cumulative Days 2–{day}: ~{cum_b:.2f}B libFuzzer executions on persistent corpus. "
        f"Day {day} HTML live on the public ledger."
    ),
    "action": (
        f"Day {day} report: https://hackme.tech/reports/oss-cve-watch/day{day:02d}.html"
        f" · Series hub: https://hackme.tech/reports/oss-cve-watch/"
    ),
    "tags": ["research", "oss-cve", "fuzzing", "nghttp2", f"day{day:02d}"],
    "status": "published",
    "telegram": {
        "headline": f"OSS CVE Watch · Day {day}/14 · nghttp2",
        "lead": f"{hours:.1f}h libFuzzer + ASAN · {verdict}",
        "bullets": [
            f"{iters:,} executions · ~{eps:,.0f} exec/s · {hours:.1f}h",
            f"corpus {corp} ({corp_kb}KB) · {cov} edges · ASAN={asan}",
            f"Days 2–{day} cumulative ~{cum_b:.2f}B exec",
            "CLEAN = no ASAN heap crash in budget — not proven secure",
        ],
        "footer": f"hackme.tech/reports/oss-cve-watch/day{day:02d}.html",
    },
    "discord": {
        "title": f"Research · OSS CVE Watch Day {day}",
        "body": (
            f"**nghttp2 · libFuzzer · Day {day}/14**\\n\\n"
            f"• **{iters:,}** executions · **{hours:.1f}h** · **~{eps:,.0f} exec/s**\\n"
            f"• Corpus **{corp}** ({corp_kb}KB) · **{cov}** coverage edges\\n"
            f"• **{verdict}** — {asan} ASAN heap crashes\\n"
            f"• Days 2–{day} cumulative: **~{cum_b:.2f}B** exec\\n\\n"
            f"**Report:** https://hackme.tech/reports/oss-cve-watch/day{day:02d}.html"
        ),
        "ping": "#research",
    },
    "links": {
        "hub": "https://hackme.tech/reports/oss-cve-watch/",
        f"day{day:02d}": f"https://hackme.tech/reports/oss-cve-watch/day{day:02d}.html",
    },
}

for name in ("web/site/assets/news.json", "web/site/assets/news-feed.json"):
    p = Path(name)
    data = json.loads(p.read_text())
    items = [x for x in (data.get("items") or []) if x.get("id") != nid]
    data["items"] = [item] + items
    p.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\\n")
    print(f"news upsert {nid} → {name} (items={len(data['items'])})")

# Research hub cards → current day
research = Path("web/site/research.html")
text = research.read_text()
block = f"""    <section class=\"panel glass research-section\">
      <div class=\"research-section-head\">
        <div>
          <h2>OSS CVE Watch · nghttp2</h2>
          <p class=\"subtle\">One repo · 14 days · Day 2+ libFuzzer depth · honest CLEAN ledger</p>
        </div>
        <a class=\"btn btn-secondary\" href=\"./reports/oss-cve-watch/\" style=\"font-size:0.82rem;padding:0.45rem 0.9rem\">Series hub</a>
      </div>
      <div class=\"research-grid\">
        <a class=\"research-card\" href=\"./reports/oss-cve-watch/\">
          <span class=\"research-tag live\">Day {day}/14 · published</span>
          <h3>nghttp2 HTTP/2 parser</h3>
          <p>Session <code class=\"notranslate\">mem_recv</code> · persistent corpus · Days 2–{day} cumulative ~{cum_b:.2f}B libFuzzer exec · {verdict}.</p>
          <div class=\"research-stats notranslate\"><span><b>~{cum_b:.2f}B</b> exec</span><span><b>{asan}</b> ASAN</span><span>{verdict}</span></div>
        </a>
        <a class=\"research-card\" href=\"./reports/oss-cve-watch/day{day:02d}.html\">
          <span class=\"research-tag series\">Day {day} ledger</span>
          <h3>Day {day} · {iters_b:.2f}B · {hours:.1f}h</h3>
          <p>libFuzzer + ASAN · ~{eps:,.0f} exec/s · corpus {corp} · ASAN={asan} — verdict {verdict}.</p>
          <div class=\"research-stats notranslate\"><span><b>{iters_b:.2f}B</b> exec</span><span><b>{asan}</b> ASAN</span></div>
        </a>
      </div>
    </section>"""
start = text.find("<h2>OSS CVE Watch · nghttp2</h2>")
if start < 0:
    raise SystemExit("research.html: OSS CVE Watch heading not found")
sec_start = text.rfind("<section", 0, start)
sec_end = text.find("</section>", start)
if sec_start < 0 or sec_end < 0:
    raise SystemExit("research.html: OSS CVE section bounds not found")
sec_end += len("</section>")
research.write_text(text[:sec_start] + block + text[sec_end:])
print(f"research.html updated → Day {day}/14")

# X draft (local, gitignored docs/social/)
social = Path("docs/social")
social.mkdir(parents=True, exist_ok=True)
draft = social / f"OSS_CVE_WATCH_DAY{day:02d}_X.txt"
draft.write_text(
    f"""1/{day} OSS CVE Watch · Day {day}/14 · nghttp2

{hours:.1f}h libFuzzer + ASAN on HTTP/2 session mem_recv.
Verdict: {verdict} — {asan} heap crashes.

https://hackme.tech/reports/oss-cve-watch/day{day:02d}.html

2/{day}
• {iters:,} executions
• ~{eps:,.0f} exec/s · {hours:.1f}h
• corpus {corp} ({corp_kb}KB) · {cov} edges · ASAN = {asan}

3/{day}
{verdict} = no ASAN heap corruption in budget — not “proven secure.”
No CVE id without coordinated disclosure.

4/{day}
Days 2–{day}: ~{cum_b:.2f}B libFuzzer exec on the same corpus.
Hub: https://hackme.tech/reports/oss-cve-watch/
"""
)
print(f"X draft → {draft}")
print(f"NEWS_ID={nid}")
print(f"VERDICT={verdict}")
PY

# shellcheck disable=SC1091
source "$ROOT/scripts/ops/_deploy_ssh_retry.sh" 2>/dev/null || true
log "deploy site"
NODE_SSH="$NODE_SSH" NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}" SKIP_DIST=1 \
  bash "$ROOT/scripts/ops/deploy_hackme_site.sh"

NEWS_ID="$(python3 -c "import json;print(json.load(open('web/site/assets/news.json'))['items'][0]['id'])")"
log "telegram FORCE_NEWS_ID=$NEWS_ID"
FORCE_NEWS_ID="$NEWS_ID" NODE_SSH="$NODE_SSH" bash "$ROOT/scripts/ops/publish_news_to_telegram.sh" || {
  log "WARN: telegram publish failed — site still deployed; retry: FORCE_NEWS_ID=$NEWS_ID bash scripts/ops/publish_news_to_telegram.sh"
}

log "verify live"
UA='Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36'
fail=0
soft_fail=0
for u in \
  "https://hackme.tech/reports/oss-cve-watch/day$(printf '%02d' "$DAY").html" \
  "https://hackme.tech/reports/oss-cve-watch/" \
  "https://hackme.tech/research.html" \
  "https://hackme.tech/news.html"
do
  code="$(curl -sS -A "$UA" -o /tmp/cve_live_check -w '%{http_code}' --max-time 45 "$u" || echo err)"
  if [[ "$code" != "200" ]]; then
    log "FAIL $code $u"
    fail=1
  else
    log "OK  $code $u"
  fi
done
# Large news.json often truncates over CDN — verify via VPS, soft-fail public curl.
code="$(curl -sS -A "$UA" -o /tmp/cve_live_news -w '%{http_code}' --max-time 60 "https://hackme.tech/assets/news.json" || echo err)"
if [[ "$code" != "200" ]]; then
  log "WARN public news.json $code (will verify via VPS)"
  soft_fail=1
else
  log "OK  $code https://hackme.tech/assets/news.json"
fi
# news id present on live feed (via VPS to avoid CDN flake)
ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" \
  "curl -fsS --max-time 30 -H 'Accept-Encoding: identity' 'http://127.0.0.1/assets/news.json' || curl -fsS --max-time 30 -H 'Accept-Encoding: identity' 'https://hackme.tech/assets/news.json'" \
  | python3 -c "import json,sys; d=json.load(sys.stdin); print('live_top', d['items'][0]['id']); assert d['items'][0]['id'].startswith('$DATE_LOCAL-oss-cve-watch-day'), d['items'][0]['id']"

if [[ "$GIT_PUSH" == "1" ]]; then
  log "git commit + push"
  git add \
    "web/site/reports/oss-cve-watch/" \
    web/site/assets/news.json \
    web/site/assets/news-feed.json \
    web/site/research.html \
    || true
  if git diff --cached --quiet; then
    log "nothing to commit"
  else
    GIT_AUTHOR_NAME='jokeez' GIT_AUTHOR_EMAIL='dney777666@gmail.com' \
    GIT_COMMITTER_NAME='jokeez' GIT_COMMITTER_EMAIL='dney777666@gmail.com' \
      git commit -m "$(cat <<EOF
Publish OSS CVE Watch Day $(printf '%02d' "$DAY"): nghttp2 libFuzzer ledger + news.

EOF
)"
    git push origin HEAD
  fi
fi

[[ "$fail" -eq 0 ]] || exit 1
if [[ "$soft_fail" -ne 0 ]]; then
  log "WARN soft verify issues (non-fatal)"
fi
log "DONE — day $DAY live on hackme.tech + news"
