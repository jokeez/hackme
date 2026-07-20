#!/usr/bin/env bash
# Structured away journal: pool, settlement, libheif, services → TSV + action items.
# Timer: hackme-away-journal.timer (every 20 min). Review: away_return_briefing.sh
set -uo pipefail
ROOT="${HACKME_ROOT:-/opt/hackme}"
JDIR="$ROOT/reports/away-journal"
mkdir -p "$JDIR/daily" "$ROOT/logs"

python3 - "$ROOT" "$JDIR" <<'PY'
import json, os, re, sqlite3, subprocess, sys, time, urllib.request
from datetime import datetime, timezone
from pathlib import Path

root = Path(sys.argv[1])
jdir = Path(sys.argv[2])
now = int(time.time())
ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
day = ts[:10]

def get(url, timeout=12):
    try:
        with urllib.request.urlopen(url, timeout=timeout) as r:
            return json.load(r)
    except Exception as e:
        return {"_error": str(e)}

def sh(cmd):
    try:
        return subprocess.check_output(cmd, shell=True, text=True, stderr=subprocess.DEVNULL, timeout=15).strip()
    except Exception:
        return ""

actions = []
def flag(sev, area, msg, detail=""):
    actions.append({"ts": ts, "epoch": now, "severity": sev, "area": area, "msg": msg, "detail": detail})

# --- services ---
svc = {}
for u in ("hackme-node", "hackme-coordinator", "hackme-workerfuzz", "hackme-libheif-24h"):
    svc[u] = sh(f"systemctl is-active {u} 2>/dev/null") or "unknown"
for u, st in svc.items():
    if st != "active":
        flag("critical" if u in ("hackme-node", "hackme-coordinator") else "warn", "service", f"{u} not active", st)

# --- pool ---
stats = get("http://127.0.0.1:18081/api/work/stats")
pool_err = stats.pop("_error", None)
workers = stats.get("workers") or {}
gh = sum(float(w.get("hashrate_gh_s") or 0) for w in workers.values())
online = sum(1 for w in workers.values() if w.get("online"))
n_workers = len(workers)
payout = sum(float(w.get("payout_hmc") or 0) for w in workers.values())
mode = stats.get("scheduler_mode") or "?"
leases = int(stats.get("active_leases_count") or 0)
ack = float(stats.get("ack_latency_ms") or 0)

if pool_err:
    flag("critical", "pool", "coordinator stats unreachable", pool_err)
elif gh < 80:
    flag("warn", "pool", f"hashrate low {gh:.1f} GH/s", f"workers={n_workers} online={online}")
elif online < max(1, n_workers - 2):
    flag("warn", "pool", f"workers offline {online}/{n_workers}")

offline = [wid for wid, w in workers.items() if not w.get("online")]
if offline and len(offline) <= 5:
    flag("info", "pool", "offline workers", ", ".join(offline))

# --- settlement log tail ---
settle_log = root / "logs" / "settle_worker_payouts.log"
settle_errs = 0
if settle_log.is_file():
    tail = settle_log.read_text(errors="replace").splitlines()[-40:]
    settle_errs = sum(1 for ln in tail if "ERROR" in ln or "not confirmed" in ln)
    if settle_errs >= 3:
        flag("warn", "settlement", f"{settle_errs} recent settle errors in log tail", tail[-1][:200] if tail else "")

# --- open orders ---
try:
    st = get("http://127.0.0.1:18080/api/status?lite=1")
    admin = ""
    env = (root / ".env.vps").read_text(errors="replace") if (root / ".env.vps").is_file() else ""
    for ln in env.splitlines():
        if ln.startswith("HACKME_ADMIN_TOKEN="):
            admin = ln.split("=", 1)[1].strip()
            break
    open_orders = 0
    stuck = []
    if admin:
        req = urllib.request.Request(
            "http://127.0.0.1:18080/api/tasks",
            headers={"X-Hackme-Admin-Token": admin},
        )
        with urllib.request.urlopen(req, timeout=12) as r:
            tasks = json.load(r).get("tasks") or []
        for t in tasks:
            if (t.get("status") or "").lower() == "open":
                open_orders += 1
                prog = int(t.get("progress_count") or 0)
                tgt = int(t.get("target_solves") or 0)
                if tgt and prog == 0 and (now - int(t.get("created_at") or now)) > 3600:
                    stuck.append(t.get("id", "?")[:40])
    if stuck:
        flag("warn", "orders", f"{len(stuck)} PoH orders stuck at 0 progress", "; ".join(stuck[:3]))
except Exception as e:
    open_orders = -1
    flag("info", "orders", "could not scan open orders", str(e)[:120])

# --- libheif ---
fuzzer = sh("pgrep -f 'file_fuzzer.*oss-cve-libfuzzer/libheif/corpus' | head -1")
cadence_st = svc.get("hackme-libheif-24h", "?")
libheif_day = 0
libheif_remaining_h = 0
cadence_path = root / "reports/oss-cve-watch-libheif/cadence.json"
if cadence_path.is_file():
    c = json.loads(cadence_path.read_text())
    anchor = int(c.get("anchor_epoch") or 0)
    day_sec = int(c.get("day_sec") or 86400)
    start = int(c.get("start_day") or 1)
    if anchor:
        libheif_day = start + max(0, now - anchor) // day_sec
        deadline = anchor + (max(0, now - anchor) // day_sec + 1) * day_sec
        libheif_remaining_h = max(0, deadline - now) / 3600

if not fuzzer and cadence_st == "active":
    flag("critical", "libheif", "file_fuzzer down while cadence active")
elif not fuzzer:
    flag("warn", "libheif", "file_fuzzer not running")

crash_dir = root / "reports/oss-cve-libfuzzer/libheif/crashes"
asan_new = 0
if crash_dir.is_dir():
    for p in crash_dir.glob("crash-*"):
        try:
            if p.stat().st_mtime > now - 1200:
                asan_new += 1
        except OSError:
            pass
if asan_new:
    flag("info", "libheif", f"{asan_new} new crash artifact(s) last 20min — review ASAN", str(crash_dir))

day_html = root / f"web/site/reports/oss-cve-watch-libheif/day{libheif_day:02d}.html"
if libheif_remaining_h < 2 and libheif_day <= 14 and not day_html.is_file():
    flag("warn", "libheif", f"Day {libheif_day} deadline <2h, day HTML not published yet")

# --- fuzz attribution sanity ---
wf_env = (root / ".env.workerfuzz").read_text(errors="replace") if (root / ".env.workerfuzz").is_file() else ""
if "719006d93916ad52" in wf_env:
    flag("critical", "fuzz", "workerfuzz seed still maps to treasury DevFee — rotate key")

# --- TSV row ---
tsv = jdir / "metrics.tsv"
if not tsv.is_file() or tsv.stat().st_size == 0:
    tsv.write_text("ts\tgh_s\tworkers\tonline\tpayout_hmc\tmode\tleases\tack_ms\topen_orders\tlibheif_day\tfuzzer\tcadence\tsettle_err_tail\tactions\n")
row = "\t".join([
    ts, f"{gh:.1f}", str(n_workers), str(online), f"{payout:.3f}",
    mode, str(leases), f"{ack:.2f}", str(open_orders),
    str(libheif_day), "up" if fuzzer else "down", cadence_st,
    str(settle_errs), str(len(actions)),
]) + "\n"
with tsv.open("a") as f:
    f.write(row)

# --- action items jsonl ---
if actions:
    with (jdir / "action_items.jsonl").open("a") as f:
        for a in actions:
            f.write(json.dumps(a, ensure_ascii=False) + "\n")

# --- snapshot json (latest) ---
snap = {
    "ts": ts, "epoch": now,
    "pool": {"gh_s": gh, "workers": n_workers, "online": online, "payout_hmc": payout, "mode": mode, "leases": leases, "ack_ms": ack},
    "services": svc,
    "libheif": {"day": libheif_day, "remaining_h": round(libheif_remaining_h, 2), "fuzzer": bool(fuzzer), "cadence": cadence_st},
    "open_orders": open_orders,
    "settle_err_tail": settle_errs,
    "actions_this_sample": actions,
}
(jdir / "latest.json").write_text(json.dumps(snap, indent=2) + "\n")

# --- daily rollup append ---
daily = jdir / "daily" / f"{day}.jsonl"
with daily.open("a") as f:
    f.write(json.dumps({"ts": ts, "gh_s": gh, "online": online, "actions": len(actions), "fuzzer": bool(fuzzer)}) + "\n")

print(f"[away-journal] {ts} gh={gh:.1f} workers={online}/{n_workers} libheif_d{libheif_day} fuzzer={'up' if fuzzer else 'DOWN'} actions={len(actions)}")
PY

# prune action_items if huge
AI="$JDIR/action_items.jsonl"
if [[ -f "$AI" ]] && [[ $(wc -l <"$AI") -gt 2000 ]]; then
  tail -1500 "$AI" >"${AI}.tmp" && mv "${AI}.tmp" "$AI"
fi
