#!/usr/bin/env bash
# One week ops measurement sample (hub). Pool + fuzz marketplace + per-miner
# unpaid + host load/disk + light exchange HTTP probes.
#
# Timer: hackme-week-ops-metrics.timer (every 15 min for ~7d window).
# Review: bash scripts/ops/week_ops_briefing.sh
set -uo pipefail
ROOT="${HACKME_ROOT:-/opt/hackme}"
JDIR="${WEEK_OPS_DIR:-$ROOT/reports/week-ops-metrics}"
mkdir -p "$JDIR/miners" "$JDIR/campaigns" "$JDIR/daily" "$ROOT/logs"

python3 - "$ROOT" "$JDIR" <<'PY'
import http.client, json, os, sqlite3, subprocess, sys, time, urllib.error, urllib.request
from datetime import datetime, timezone, timedelta
from pathlib import Path

root = Path(sys.argv[1])
jdir = Path(sys.argv[2])
now = int(time.time())
ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
day = ts[:10]
started_marker = jdir / "STARTED.json"
if not started_marker.is_file():
    started_marker.write_text(json.dumps({
        "started_at": ts,
        "epoch": now,
        "planned_end_at": (datetime.now(timezone.utc) + timedelta(days=7)).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "interval_note": "15min samples via hackme-week-ops-metrics.timer",
        "focus": ["pool_poh", "fuzz_orders_runs", "per_miner_unpaid", "settlement", "host_disk_load", "exchange_http"],
    }, indent=2) + "\n")

def get_json(url, timeout=12, headers=None):
    try:
        req = urllib.request.Request(url, headers=headers or {})
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.load(r), None, getattr(r, "status", 200)
    except Exception as e:
        return {}, str(e), 0

def http_probe(url, timeout=12, resolve_local=False):
    """Probe public paths. From hub, hit local TLS with Host:hackme.tech to avoid CF 403."""
    import ssl
    t0 = time.time()
    try:
        if resolve_local and url.startswith("https://hackme.tech"):
            path = url[len("https://hackme.tech"):] or "/"
            ctx = ssl.create_default_context()
            ctx.check_hostname = False
            ctx.verify_mode = ssl.CERT_NONE
            conn = http.client.HTTPSConnection("127.0.0.1", 443, context=ctx, timeout=timeout)
            conn.request("GET", path, headers={"Host": "hackme.tech", "User-Agent": "hackme-week-ops/1"})
            resp = conn.getresponse()
            body = resp.read(256)
            code = resp.status
            conn.close()
            return {"url": url, "code": code, "ms": round((time.time() - t0) * 1000, 1), "bytes": len(body), "via": "local-tls"}
        req = urllib.request.Request(url, method="GET", headers={"User-Agent": "hackme-week-ops/1"})
        with urllib.request.urlopen(req, timeout=timeout) as r:
            body = r.read(256)
            return {"url": url, "code": getattr(r, "status", 200), "ms": round((time.time() - t0) * 1000, 1), "bytes": len(body)}
    except urllib.error.HTTPError as e:
        return {"url": url, "code": e.code, "ms": round((time.time() - t0) * 1000, 1), "bytes": 0, "error": str(e)}
    except Exception as e:
        return {"url": url, "code": 0, "ms": round((time.time() - t0) * 1000, 1), "bytes": 0, "error": str(e)}

def sh(cmd):
    try:
        return subprocess.check_output(cmd, shell=True, text=True, stderr=subprocess.DEVNULL, timeout=20).strip()
    except Exception:
        return ""

actions = []
def flag(sev, area, msg, detail=""):
    actions.append({"ts": ts, "epoch": now, "severity": sev, "area": area, "msg": msg, "detail": detail})

# --- host ---
loadavg = sh("cut -d' ' -f1-3 /proc/loadavg")
mem = sh("free -m | awk '/Mem:/{printf \"%s %s %s\",$2,$3,$7}'")
disk = sh("df -P / | awk 'NR==2{print $2,$3,$4,$5}'")
disk_opt = sh("df -P /opt/hackme 2>/dev/null | awk 'NR==2{print $5}'")
du_data = sh("du -sm /opt/hackme/data 2>/dev/null | awk '{print $1}'")
du_logs = sh("du -sm /opt/hackme/logs 2>/dev/null | awk '{print $1}'")
du_reports = sh("du -sm /opt/hackme/reports 2>/dev/null | awk '{print $1}'")
nproc = sh("nproc") or "?"
host = {
    "loadavg": loadavg,
    "nproc": nproc,
    "mem_mb_total_used_avail": mem,
    "disk_blocks_used_avail_usepct": disk,
    "disk_opt_usepct": disk_opt,
    "du_data_mb": int(du_data or 0),
    "du_logs_mb": int(du_logs or 0),
    "du_reports_mb": int(du_reports or 0),
}
try:
    use = int((disk.split()[-1] if disk else "0").rstrip("%") or 0)
    if use >= 90:
        flag("critical", "host", f"disk {use}% full", disk)
    elif use >= 80:
        flag("warn", "host", f"disk {use}%", disk)
    la1 = float(loadavg.split()[0]) if loadavg else 0
    cores = int(nproc) if str(nproc).isdigit() else 2
    if la1 > cores * 2:
        flag("warn", "host", f"load high {la1} on {cores} cores", loadavg)
except Exception:
    pass

# --- services ---
svc = {}
for u in ("hackme-node", "hackme-coordinator", "hackme-workerfuzz", "hackme-libheif-24h", "nginx"):
    svc[u] = sh(f"systemctl is-active {u} 2>/dev/null") or "unknown"
    if svc[u] != "active" and u in ("hackme-node", "hackme-coordinator", "nginx"):
        flag("critical", "service", f"{u} not active", svc[u])
    elif svc[u] != "active":
        flag("warn", "service", f"{u} not active", svc[u])

# --- pool / miners (prefer public details=0 on :18083, fallback :18081) ---
stats, err, _ = get_json("http://127.0.0.1:18083/api/work/stats?details=0")
if err or not stats:
    stats, err, _ = get_json("http://127.0.0.1:18081/api/work/stats?details=0")
if err and not stats:
    flag("critical", "pool", "coordinator stats unreachable", err or "")
    stats = {}

workers = stats.get("workers") or {}
rigs = stats.get("active_rigs") or []
gh = float(stats.get("pool_hashrate_gh_s") or 0)
if not gh and workers:
    gh = sum(float((w or {}).get("hashrate_gh_s") or 0) for w in workers.values())
online = int(stats.get("workers_online") or sum(1 for w in workers.values() if isinstance(w, dict) and w.get("online")))
n_workers = int(stats.get("workers_count") or len(workers) or len(rigs))
fleet_unpaid = sum(float((w or {}).get("payout_hmc") or 0) for w in workers.values() if isinstance(w, dict))
mode = stats.get("scheduler_mode") or "?"
orders_active = bool(stats.get("orders_active"))
leases = int(stats.get("active_leases_count") or 0)
ack = float(stats.get("ack_latency_ms") or 0)
signed_acc = int(stats.get("signed_submits_accepted") or 0)
signed_rej = int(stats.get("signed_submits_rejected") or 0)
total_payout_hmc = float(stats.get("total_payout_hmc") or fleet_unpaid)
total_payout_sup = float(stats.get("total_payout_sup") or 0)

miners = []
# prefer workers map; else rigs
if workers:
    for wid, w in workers.items():
        if not isinstance(w, dict):
            continue
        miners.append({
            "worker_id": wid,
            "online": bool(w.get("online")),
            "gh_s": float(w.get("hashrate_gh_s") or 0),
            "unpaid_hmc": float(w.get("payout_hmc") or 0),
            "unpaid_sup": float(w.get("payout_sup") or 0),
            "signed_submits": int(w.get("signed_submits") or 0),
            "accepted_attempts": int(w.get("accepted_attempts") or 0),
            "accepted_hits": int(w.get("accepted_hits") or 0),
            "payout_address": (w.get("payout_address") or "")[:32],
        })
else:
    for r in rigs:
        if not isinstance(r, dict):
            continue
        miners.append({
            "worker_id": r.get("worker_id") or r.get("name"),
            "online": bool(r.get("online")),
            "gh_s": float(r.get("hashrate_gh_s") or 0),
            "unpaid_hmc": None,
            "unpaid_sup": None,
            "signed_submits": None,
            "accepted_attempts": None,
            "accepted_hits": None,
            "payout_address": "",
        })
# settled lifetime from node settlement state (PoH+orders mixed ledger; no per-kind split yet)
settled_map = {}
settle_path = root / "data" / "worker_settlement_state.json"
if settle_path.is_file():
    try:
        settle_doc = json.loads(settle_path.read_text())
        for wid, row in (settle_doc.get("workers") or {}).items():
            if not isinstance(row, dict):
                continue
            settled_map[wid] = {
                "settled_hmc": float(row.get("settled_hmc") or 0),
                "settled_sup": float(row.get("settled_sup") or 0),
                "last_settle_unix": row.get("last_settle_unix"),
                "last_tx_hash": (row.get("last_tx_hash") or "")[:16],
            }
    except Exception as e:
        flag("warn", "settlement", "worker_settlement_state read failed", str(e)[:160])

for m in miners:
    wid = m.get("worker_id")
    s = settled_map.get(wid) or {}
    m["settled_hmc"] = s.get("settled_hmc")
    m["settled_sup"] = s.get("settled_sup")
    m["last_settle_unix"] = s.get("last_settle_unix")
    # approximate: unpaid is accrual not yet settled; settled is paid-out lifetime
    m["lifetime_hmc_approx"] = round(float(m.get("unpaid_hmc") or 0) + float(s.get("settled_hmc") or 0), 6)

miners.sort(key=lambda m: (-(m.get("unpaid_hmc") or 0), -(m.get("gh_s") or 0)))
settled_online = sum(float((settled_map.get(m.get("worker_id")) or {}).get("settled_hmc") or 0) for m in miners)

# --- marketplace API + SQLite fuzz_campaigns (authoritative for runs) ---
campaigns = []
for url in (
    "http://127.0.0.1:18080/api/fuzz/marketplace",
    "http://127.0.0.1:8080/api/fuzz/marketplace",
):
    data, e, code = get_json(url)
    if data.get("campaigns"):
        campaigns = data.get("campaigns") or []
        break
camp_by_id = {}
for c in campaigns:
    if isinstance(c, dict) and c.get("id"):
        camp_by_id[str(c["id"])] = c

db_camps = []
db_path = root / "data" / "hackme.db"
if db_path.is_file():
    try:
        con = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True, timeout=5)
        cur = con.execute(
            "SELECT id, status, campaign_type, title, budget_runs, summary_json, created_at "
            "FROM fuzz_campaigns WHERE status IN ('running','active','queued','open','settling') "
            "OR created_at > strftime('%s','now','-7 days') "
            "ORDER BY created_at DESC LIMIT 80"
        )
        for row in cur.fetchall():
            cid, st, ctype, title, br, summary_json, created_at = row
            summary = {}
            try:
                summary = json.loads(summary_json or "{}")
            except Exception:
                summary = {}
            rd = int(summary.get("runs_done") or 0)
            api = camp_by_id.get(str(cid)) or {}
            if api.get("runs_done") is not None:
                rd = int(api.get("runs_done") or rd)
            item = {
                "id": cid,
                "title": (title or api.get("title") or "")[:80],
                "status": st,
                "campaign_type": ctype,
                "runs_done": rd,
                "budget_runs": int(br or api.get("budget_runs") or 0),
                "per_run_hmc": api.get("per_run_hmc"),
                "budget_hmc": api.get("budget_hmc"),
                "findings": summary.get("findings") if summary.get("findings") is not None else api.get("findings"),
                "pool": bool(api.get("pool")) if api else None,
                "created_at": created_at,
            }
            db_camps.append(item)
            camp_by_id[str(cid)] = item
        con.close()
    except Exception as e:
        flag("warn", "fuzz", "sqlite fuzz_campaigns read failed", str(e)[:160])

camp_slim = list(camp_by_id.values()) if camp_by_id else []
# prefer db-enriched list order
if db_camps:
    camp_slim = db_camps
runs_total = 0
runs_budget = 0
running_n = 0
stuck_zero = 0
for c in camp_slim:
    if not isinstance(c, dict):
        continue
    rd = int(c.get("runs_done") or 0)
    br = int(c.get("budget_runs") or 0)
    st = str(c.get("status") or "")
    if "run" in st.lower() or st.lower() in ("active", "open", "queued"):
        running_n += 1
        runs_total += rd
        runs_budget += br
        if br >= 1000 and rd == 0:
            stuck_zero += 1
if stuck_zero:
    flag("warn", "fuzz", f"{stuck_zero} running campaign(s) still at 0 runs", "possible dig monopoly / no workerfuzz")

# --- PoH open orders ---
open_orders = 0
admin = ""
for env_name in (".env.vps", ".env", ".env.node"):
    p = root / env_name
    if not p.is_file():
        continue
    for ln in p.read_text(errors="replace").splitlines():
        if ln.startswith("HACKME_ADMIN_TOKEN="):
            admin = ln.split("=", 1)[1].strip()
            break
    if admin:
        break
if admin:
    data, e, _ = get_json("http://127.0.0.1:18080/api/tasks", headers={"X-Hackme-Admin-Token": admin})
    tasks = data.get("tasks") or []
    open_orders = sum(1 for t in tasks if isinstance(t, dict) and str(t.get("status") or "").lower() == "open")

# --- tip ---
st, _, _ = get_json("http://127.0.0.1:18080/api/status")
tip = st.get("tip_height") or st.get("display_tip_height")
version = st.get("version")

# --- settlement log ---
settle_errs = 0
for name in ("settle_worker_payouts.log", "settlement-autopilot.log"):
    p = root / "logs" / name
    if p.is_file():
        tail = p.read_text(errors="replace").splitlines()[-50:]
        settle_errs += sum(1 for ln in tail if "ERROR" in ln or "not confirmed" in ln)
if settle_errs >= 5:
    flag("warn", "settlement", f"{settle_errs} recent settle ERROR/not-confirmed lines")

# --- libheif ---
fuzzer = bool(sh("pgrep -f 'file_fuzzer.*oss-cve-libfuzzer/libheif/corpus' | head -1"))
libheif_day = 0
meta = root / "web/site/reports/oss-cve-watch-libheif/meta.json"
if meta.is_file():
    try:
        libheif_day = int(json.loads(meta.read_text()).get("current_day") or 0)
    except Exception:
        pass
if not fuzzer:
    flag("warn", "libheif", "file_fuzzer not running")

# --- exchange / listing light probes (local TLS → avoid Cloudflare bot 403 from VPS IP) ---
exchange_probes = [
    http_probe("https://hackme.tech/exchange.html", resolve_local=True),
    http_probe("https://hackme.tech/listing.html", resolve_local=True),
    http_probe("https://hackme.tech/api/status", resolve_local=True),
    http_probe("https://hackme.tech/api/pool/stats", resolve_local=True),
    http_probe("https://hackme.tech/downloads.html", resolve_local=True),
]
for p in exchange_probes:
    if p.get("code") not in (200, 301, 302):
        flag("warn", "exchange", f"probe {p.get('code')} {p.get('url')}", p.get("error") or "")

# --- write artifacts ---
snap = {
    "ts": ts,
    "epoch": now,
    "host": host,
    "services": svc,
    "hub": {"version": version, "tip": tip},
    "pool": {
        "gh_s": round(gh, 2),
        "workers": n_workers,
        "online": online,
        "mode": mode,
        "orders_active": orders_active,
        "leases": leases,
        "ack_ms": round(ack, 3),
        "signed_acc": signed_acc,
        "signed_rej": signed_rej,
        "fleet_unpaid_hmc": round(fleet_unpaid, 6),
        "total_payout_hmc": round(total_payout_hmc, 6),
        "total_payout_sup": round(total_payout_sup, 6),
        "settled_hmc_online_miners": round(settled_online, 6),
        "open_poh_orders": open_orders,
    },
    "fuzz": {
        "campaigns_n": len(camp_slim),
        "running_n": running_n,
        "runs_done_sum": runs_total,
        "budget_runs_sum": runs_budget,
        "stuck_zero_running": stuck_zero,
        "campaigns": camp_slim[:30],
    },
    "miners": miners,
    "libheif": {"current_day": libheif_day, "fuzzer": fuzzer},
    "exchange_probes": exchange_probes,
    "settle_err_tail": settle_errs,
    "actions": actions,
}
(jdir / "latest.json").write_text(json.dumps(snap, indent=2) + "\n")
with (jdir / "snapshots.jsonl").open("a") as f:
    f.write(json.dumps(snap, ensure_ascii=False) + "\n")
with (jdir / "miners" / f"{day}.jsonl").open("a") as f:
    f.write(json.dumps({"ts": ts, "miners": miners}, ensure_ascii=False) + "\n")
with (jdir / "campaigns" / f"{day}.jsonl").open("a") as f:
    f.write(json.dumps({"ts": ts, "campaigns": camp_slim}, ensure_ascii=False) + "\n")

tsv = jdir / "metrics.tsv"
if not tsv.is_file() or tsv.stat().st_size == 0:
    tsv.write_text(
        "ts\tgh_s\tonline\tworkers\tmode\torders_active\tfleet_unpaid_hmc\trunning_camps\truns_done_sum\tstuck_zero\tdisk_pct\tload1\tsettle_errs\tfuzzer\tactions\n"
    )
disk_pct = (disk.split()[-1] if disk else "?")
load1 = (loadavg.split()[0] if loadavg else "?")
row = "\t".join([
    ts, f"{gh:.1f}", str(online), str(n_workers), str(mode), str(int(orders_active)),
    f"{fleet_unpaid:.6f}", str(running_n), str(runs_total), str(stuck_zero),
    str(disk_pct), str(load1), str(settle_errs), "up" if fuzzer else "down", str(len(actions)),
]) + "\n"
with tsv.open("a") as f:
    f.write(row)

if actions:
    with (jdir / "action_items.jsonl").open("a") as f:
        for a in actions:
            f.write(json.dumps(a, ensure_ascii=False) + "\n")

with (jdir / "daily" / f"{day}.jsonl").open("a") as f:
    f.write(json.dumps({
        "ts": ts, "gh_s": round(gh, 1), "online": online,
        "fleet_unpaid_hmc": round(fleet_unpaid, 6),
        "running_camps": running_n, "runs_done_sum": runs_total,
        "disk": disk_pct, "load1": load1, "actions": len(actions),
    }, ensure_ascii=False) + "\n")

print(
    f"[week-ops] {ts} gh={gh:.1f} online={online}/{n_workers} unpaid={fleet_unpaid:.4f} "
    f"camps_run={running_n} runs={runs_total} stuck0={stuck_zero} disk={disk_pct} load={load1} actions={len(actions)}"
)
PY

# keep jsonl bounded (~8 days of 15min ≈ 800 samples; keep last 1200)
JSONL="$JDIR/snapshots.jsonl"
if [[ -f "$JSONL" ]] && [[ $(wc -l <"$JSONL") -gt 1500 ]]; then
  tail -1200 "$JSONL" >"${JSONL}.tmp" && mv "${JSONL}.tmp" "$JSONL"
fi
AI="$JDIR/action_items.jsonl"
if [[ -f "$AI" ]] && [[ $(wc -l <"$AI") -gt 3000 ]]; then
  tail -2000 "$AI" >"${AI}.tmp" && mv "${AI}.tmp" "$AI"
fi
