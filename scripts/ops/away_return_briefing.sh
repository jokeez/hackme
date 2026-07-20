#!/usr/bin/env bash
# Generate return briefing from away journal (run when operator is back).
#   bash scripts/ops/away_return_briefing.sh
#   NODE_SSH=hackme-vps bash scripts/ops/away_return_briefing.sh
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-}"

run_local() {
  JDIR="$ROOT/reports/away-journal"
  OUT="$JDIR/RETURN_BRIEFING.md"
  python3 - "$JDIR" "$OUT" <<'PY'
import json, sys
from collections import Counter, defaultdict
from pathlib import Path
from datetime import datetime

jdir = Path(sys.argv[1])
out = Path(sys.argv[2])
lines = []

def h(s): lines.append(s)

h("# Away return briefing")
h("")
h(f"Generated: {datetime.utcnow().strftime('%Y-%m-%d %H:%M UTC')}")
h("")

tsv = jdir / "metrics.tsv"
if tsv.is_file():
    rows = [ln.split("\t") for ln in tsv.read_text().splitlines()[1:] if ln.strip()]
    if rows:
        gh_vals = [float(r[1]) for r in rows if len(r) > 1]
        h("## Pool metrics (journal period)")
        h("")
        h(f"- Samples: **{len(rows)}**")
        h(f"- GH/s min/mean/max: **{min(gh_vals):.1f} / {sum(gh_vals)/len(gh_vals):.1f} / {max(gh_vals):.1f}**")
        h(f"- First sample: `{rows[0][0]}`")
        h(f"- Last sample: `{rows[-1][0]}`")
        h("")

actions = []
ai = jdir / "action_items.jsonl"
if ai.is_file():
    for ln in ai.read_text().splitlines():
        ln = ln.strip()
        if ln:
            try: actions.append(json.loads(ln))
            except: pass

if actions:
    h("## Action items (fix when back)")
    h("")
    by_sev = {"critical": [], "warn": [], "info": []}
    seen = set()
    for a in reversed(actions):
        key = (a.get("area"), a.get("msg"))
        if key in seen:
            continue
        seen.add(key)
        sev = a.get("severity") or "info"
        by_sev.setdefault(sev, []).append(a)
    for sev in ("critical", "warn", "info"):
        items = by_sev.get(sev) or []
        if not items:
            continue
        h(f"### {sev.upper()} ({len(items)})")
        h("")
        for a in items[:15]:
            h(f"- **[{a.get('area','?')}]** {a.get('msg','')} — _{a.get('ts','')}_")
            if a.get("detail"):
                h(f"  - detail: `{str(a['detail'])[:160]}`")
        h("")
else:
    h("## Action items")
    h("")
    h("_No flagged issues in journal._")
    h("")

latest = jdir / "latest.json"
if latest.is_file():
    s = json.loads(latest.read_text())
    h("## Latest snapshot")
    h("")
    p = s.get("pool") or {}
    l = s.get("libheif") or {}
    h(f"- Pool: **{p.get('gh_s',0):.1f} GH/s**, {p.get('online',0)}/{p.get('workers',0)} online, mode `{p.get('mode')}`")
    h(f"- Libheif: day **{l.get('day')}**, fuzzer **{'up' if l.get('fuzzer') else 'DOWN'}**, ~{l.get('remaining_h',0):.1f}h to day deadline")
    h(f"- Open orders: **{s.get('open_orders', '?')}**")
    h("")

h("## Raw paths")
h("")
h(f"- Metrics TSV: `{tsv}`")
h(f"- Action items: `{ai}`")
h(f"- Latest JSON: `{latest}`")
h(f"- Pool watch log: `logs/pool-away-watch.log`")
h(f"- Libheif service: `logs/hackme-libheif-24h.service.log`")
h("")

out.parent.mkdir(parents=True, exist_ok=True)
out.write_text("\n".join(lines) + "\n")
print(out)
PY
}

if [[ -n "$NODE_SSH" ]]; then
  ssh -o BatchMode=yes "$NODE_SSH" "bash $ROOT/scripts/ops/away_return_briefing.sh" 2>/dev/null || {
    mkdir -p "$ROOT/reports/away-journal"
    scp -o BatchMode=yes "$NODE_SSH:/opt/hackme/reports/away-journal/"* "$ROOT/reports/away-journal/" 2>/dev/null || true
    run_local
  }
  scp -o BatchMode=yes "$NODE_SSH:/opt/hackme/reports/away-journal/RETURN_BRIEFING.md" "$ROOT/reports/away-journal/" 2>/dev/null || true
else
  run_local
fi

cat "$ROOT/reports/away-journal/RETURN_BRIEFING.md" 2>/dev/null | head -60
