#!/usr/bin/env python3
"""Parse nginx access logs for HackMe OS / downloads.html interest.

Tracks:
  - GET /downloads.html (page views, unique visitors)
  - GET/HEAD /dist/.../HackMe-OS-*.iso (clicks and real downloads)

Behind Cloudflare the default combined log shows edge IPs only. Use
vps_enable_nginx_client_ip_log.sh for a sidecar log with CF-Connecting-IP.

Usage:
  nginx_downloads_interest.py report [--log PATH] [--minutes N] [--since DATE]
  nginx_downloads_interest.py live   [--log PATH] [--window-minutes N]
  nginx_downloads_interest.py tail   # read stdin (for: tail -F ... | script)
"""

from __future__ import annotations

import argparse
import hashlib
import re
import sys
from collections import defaultdict
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Iterable, Iterator, Optional

# nginx combined: IP - - [time] "METHOD path PROTO" status bytes "referer" "ua"
# Optional extended (hackme_client): ... "ua" cf_ip=1.2.3.4 xff=...
COMBINED_RE = re.compile(
    r'^(?P<ip>\S+)\s+\S+\s+\S+\s+\[(?P<time>[^\]]+)\]\s+'
    r'"(?P<method>\S+)\s+(?P<path>\S+)\s+(?P<proto>[^"]+)"\s+'
    r'(?P<status>\d{3})\s+(?P<bytes>\d+|-)\s+'
    r'"(?P<referer>[^"]*)"\s+"(?P<ua>[^"]*)"'
    r'(?:\s+cf_ip=(?P<cf_ip>\S+)(?:\s+xff=(?P<xff>\S+))?)?\s*$'
)

TIME_FMT = "%d/%b/%Y:%H:%M:%S %z"

BOT_UA_SUBSTR = (
    "bot",
    "spider",
    "crawl",
    "curl/",
    "wget/",
    "go-http-client",
    "python-requests",
    "hackme-verdict",
    "healthcheck",
    "uptimerobot",
    "googlebot",
    "bingpreview",
)

DEFAULT_ISO_SUFFIX = "HackMe-OS-0.1.0-rc11g-amd64.iso"
DEFAULT_DOWNLOADS_PATH = "/downloads.html"


@dataclass
class Event:
    ts: datetime
    ip: str
    client_ip: str
    method: str
    path: str
    status: int
    bytes_sent: int
    referer: str
    ua: str
    fingerprint: str

    @property
    def is_bot(self) -> bool:
        ua = self.ua.lower()
        return any(s in ua for s in BOT_UA_SUBSTR)


@dataclass
class Stats:
    downloads_hits: int = 0
    downloads_bots: int = 0
    iso_requests: int = 0
    iso_success: int = 0
    iso_bytes: int = 0
    iso_from_downloads: int = 0
    visitors_fp: set[str] = field(default_factory=set)
    visitors_ip: set[str] = field(default_factory=set)
    visitors_client: set[str] = field(default_factory=set)
    iso_downloaders_fp: set[str] = field(default_factory=set)
    iso_downloaders_client: set[str] = field(default_factory=set)
    iso_clickers_fp: set[str] = field(default_factory=set)
    recent: list[str] = field(default_factory=list)
    by_hour_downloads: dict[str, int] = field(default_factory=lambda: defaultdict(int))
    by_hour_iso_ok: dict[str, int] = field(default_factory=lambda: defaultdict(int))

    def record_recent(self, line: str, max_n: int = 12) -> None:
        self.recent.append(line)
        if len(self.recent) > max_n:
            self.recent.pop(0)


def parse_time(raw: str) -> datetime:
    return datetime.strptime(raw, TIME_FMT)


def fingerprint(ip: str, ua: str) -> str:
    h = hashlib.sha256(f"{ip}|{ua}".encode("utf-8", errors="replace")).hexdigest()[:12]
    return h


def parse_line(line: str) -> Optional[Event]:
    line = line.strip()
    if not line or line.startswith("#"):
        return None
    m = COMBINED_RE.match(line)
    if not m:
        return None
    try:
        ts = parse_time(m.group("time"))
    except ValueError:
        return None
    ip = m.group("ip")
    cf_ip = (m.group("cf_ip") or "").strip()
    xff = (m.group("xff") or "").strip()
    client_ip = cf_ip or (xff.split(",")[0].strip() if xff else "") or ip
    try:
        status = int(m.group("status"))
    except ValueError:
        return None
    raw_bytes = m.group("bytes")
    bytes_sent = 0 if raw_bytes == "-" else int(raw_bytes)
    ua = m.group("ua")
    fp = fingerprint(client_ip, ua)
    return Event(
        ts=ts,
        ip=ip,
        client_ip=client_ip,
        method=m.group("method").upper(),
        path=m.group("path"),
        status=status,
        bytes_sent=bytes_sent,
        referer=m.group("referer"),
        ua=ua,
        fingerprint=fp,
    )


def iso_path_matches(path: str, iso_subpath: str) -> bool:
    return iso_subpath in path or path.endswith(".iso") and "HackMe-OS" in path


def ingest(
    ev: Event,
    st: Stats,
    *,
    iso_subpath: str,
    downloads_path: str,
    include_bots: bool,
) -> None:
    if ev.is_bot and not include_bots:
        return

    hour_key = ev.ts.strftime("%Y-%m-%d %H:00")

    if ev.method == "GET" and downloads_path in ev.path and ev.status == 200:
        st.downloads_hits += 1
        if ev.is_bot:
            st.downloads_bots += 1
        else:
            st.visitors_fp.add(ev.fingerprint)
            st.visitors_ip.add(ev.ip)
            st.visitors_client.add(ev.client_ip)
            st.by_hour_downloads[hour_key] += 1
            st.record_recent(
                f"{ev.ts.strftime('%H:%M:%S')} page  client={ev.client_ip}  {short_ua(ev.ua)}"
            )

    if iso_path_matches(ev.path, iso_subpath) and ev.method in ("GET", "HEAD"):
        st.iso_requests += 1
        from_dl = "downloads.html" in ev.referer
        if from_dl:
            st.iso_from_downloads += 1
            if not ev.is_bot:
                st.iso_clickers_fp.add(ev.fingerprint)

        if ev.status in (200, 206):
            st.iso_success += 1
            st.iso_bytes += ev.bytes_sent
            if not ev.is_bot:
                st.iso_downloaders_fp.add(ev.fingerprint)
                st.iso_downloaders_client.add(ev.client_ip)
                st.by_hour_iso_ok[hour_key] += 1
                st.record_recent(
                    f"{ev.ts.strftime('%H:%M:%S')} iso {ev.status} {ev.bytes_sent}B  "
                    f"client={ev.client_ip}  {short_ua(ev.ua)}"
                )
        elif ev.status in (301, 302, 404) and from_dl:
            st.record_recent(
                f"{ev.ts.strftime('%H:%M:%S')} iso {ev.status} (click)  client={ev.client_ip}"
            )


def short_ua(ua: str, n: int = 48) -> str:
    ua = ua.replace("\n", " ")
    return ua if len(ua) <= n else ua[: n - 1] + "…"


def iter_log_lines(
    log_path: Path,
    *,
    grep_filter: Optional[str] = None,
) -> Iterator[str]:
    if grep_filter:
        import subprocess

        proc = subprocess.Popen(
            [
                "grep",
                "-E",
                grep_filter,
                str(log_path),
            ],
            stdout=subprocess.PIPE,
            text=True,
            errors="replace",
        )
        assert proc.stdout is not None
        for line in proc.stdout:
            yield line
        proc.wait()
        return

    with log_path.open("r", encoding="utf-8", errors="replace") as f:
        for line in f:
            yield line


def grep_pattern(iso_subpath: str) -> str:
    base = re.escape(DEFAULT_DOWNLOADS_PATH)
    iso = re.escape(iso_subpath)
    return f"{base}|{iso}|HackMe-OS.*\\.iso"


def filter_since(ev: Event, since: Optional[datetime], minutes: Optional[int]) -> bool:
    if since and ev.ts < since:
        return False
    if minutes is not None:
        cutoff = datetime.now(ev.ts.tzinfo) - timedelta(minutes=minutes)
        if ev.ts < cutoff:
            return False
    return True


def render_report(
    st: Stats,
    *,
    title: str,
    behind_cloudflare: bool,
    client_ip_log: bool,
) -> str:
    lines = [
        "",
        f"=== {title} ===",
        "",
        "downloads.html (GET 200, non-bot)",
        f"  page views:              {st.downloads_hits - st.downloads_bots}",
        f"  unique visitors (fp):    {len(st.visitors_fp)}",
    ]
    if client_ip_log:
        lines.append(f"  unique client IPs:       {len(st.visitors_client)}")
    else:
        lines.append(f"  unique edge IPs (nginx): {len(st.visitors_ip)}")
        if behind_cloudflare:
            lines.append(
                "  note: behind Cloudflare, edge IP ≠ end user. "
                "Run vps_enable_nginx_client_ip_log.sh for real IPs."
            )

    lines.extend(
        [
            "",
            f"HackMe OS ISO ({DEFAULT_ISO_SUFFIX})",
            f"  iso requests (GET/HEAD): {st.iso_requests}",
            f"  clicks from downloads:   {st.iso_from_downloads}",
            f"  unique clickers (fp):    {len(st.iso_clickers_fp)}",
            f"  successful (200/206):    {st.iso_success}",
            f"  unique downloaders (fp): {len(st.iso_downloaders_fp)}",
        ]
    )
    if client_ip_log:
        lines.append(f"  unique downloader IPs:   {len(st.iso_downloaders_client)}")
    lines.append(f"  bytes served (logged):   {st.iso_bytes:,}")

    if st.by_hour_downloads:
        lines.append("")
        lines.append("downloads.html by hour (non-bot views):")
        for k in sorted(st.by_hour_downloads):
            lines.append(f"  {k}  {st.by_hour_downloads[k]}")

    if st.by_hour_iso_ok:
        lines.append("")
        lines.append("ISO 200/206 by hour:")
        for k in sorted(st.by_hour_iso_ok):
            lines.append(f"  {k}  {st.by_hour_iso_ok[k]}")

    if st.recent:
        lines.append("")
        lines.append("Recent events:")
        for r in st.recent:
            lines.append(f"  {r}")

    lines.append("")
    return "\n".join(lines)


def run_report(args: argparse.Namespace) -> int:
    log_path = Path(args.log)
    if not log_path.is_file():
        print(f"error: log not found: {log_path}", file=sys.stderr)
        return 1

    iso_subpath = args.iso_subpath
    since_dt: Optional[datetime] = None
    if args.since:
        since_dt = datetime.strptime(args.since, "%Y-%m-%d").replace(tzinfo=timezone.utc)
        # nginx log on VPS is +0200; compare in local log TZ by using naive wide window
        since_dt = since_dt - timedelta(hours=12)

    client_ip_log = bool(args.client_log)

    st = Stats()
    pat = grep_pattern(iso_subpath)
    for line in iter_log_lines(log_path, grep_filter=pat if args.fast_grep else None):
        if "cf_ip=" in line:
            client_ip_log = True
        ev = parse_line(line)
        if not ev:
            continue
        if not filter_since(ev, since_dt, args.minutes):
            continue
        ingest(
            ev,
            st,
            iso_subpath=iso_subpath,
            downloads_path=args.downloads_path,
            include_bots=args.include_bots,
        )

    title = "HackMe downloads / ISO interest"
    if args.minutes:
        title += f" (last {args.minutes} min)"
    elif args.since:
        title += f" (since {args.since})"
    else:
        title += " (full filtered scan)"

    print(
        render_report(
            st,
            title=title,
            behind_cloudflare=not client_ip_log,
            client_ip_log=client_ip_log,
        )
    )
    return 0


def run_live(args: argparse.Namespace) -> int:
    import select
    import subprocess
    import time

    log_path = Path(args.log)
    st = Stats()
    window = timedelta(minutes=args.window_minutes)
    iso_subpath = args.iso_subpath
    client_ip_log = args.client_log

    print(f"Following {log_path} (window={args.window_minutes}m, Ctrl+C to stop)\n", flush=True)

    proc = subprocess.Popen(
        ["tail", "-Fn0", str(log_path)],
        stdout=subprocess.PIPE,
        text=True,
        errors="replace",
    )
    assert proc.stdout is not None

    last_paint = 0.0
    try:
        while True:
            ready, _, _ = select.select([proc.stdout], [], [], 0.5)
            if ready:
                line = proc.stdout.readline()
                if not line:
                    break
                ev = parse_line(line)
                if not ev:
                    continue
                if "cf_ip=" in line:
                    client_ip_log = True
                cutoff = datetime.now(ev.ts.tzinfo) - window
                if ev.ts < cutoff:
                    continue
                ingest(
                    ev,
                    st,
                    iso_subpath=iso_subpath,
                    downloads_path=args.downloads_path,
                    include_bots=args.include_bots,
                )

            now = time.time()
            if now - last_paint >= args.refresh_sec:
                last_paint = now
                # prune window
                cutoff = datetime.now(timezone.utc) - window
                # re-scan not feasible live; approximate with rolling stats only
                sys.stdout.write("\033[2J\033[H")  # clear screen
                print(
                    render_report(
                        st,
                        title=f"LIVE — last {args.window_minutes} min (rolling, since tail started)",
                        behind_cloudflare=not client_ip_log,
                        client_ip_log=client_ip_log,
                    ),
                    flush=True,
                )
    except KeyboardInterrupt:
        print("\nStopped.")
    return 0


def run_tail(args: argparse.Namespace) -> int:
    st = Stats()
    iso_subpath = args.iso_subpath
    client_ip_log = args.client_log
    for line in sys.stdin:
        if "cf_ip=" in line:
            client_ip_log = True
        ev = parse_line(line)
        if not ev:
            continue
        if not filter_since(ev, None, args.minutes):
            continue
        ingest(
            ev,
            st,
            iso_subpath=iso_subpath,
            downloads_path=args.downloads_path,
            include_bots=args.include_bots,
        )
    print(
        render_report(
            st,
            title="stdin batch",
            behind_cloudflare=not client_ip_log,
            client_ip_log=client_ip_log,
        )
    )
    return 0


def main() -> int:
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--log", default="/var/log/nginx/access.log", help="nginx access log path")
    common.add_argument(
        "--client-log",
        default="",
        help="if set, parse this log (expects cf_ip= from hackme_client format)",
    )
    common.add_argument("--iso-subpath", default=DEFAULT_ISO_SUFFIX, help="ISO filename substring")
    common.add_argument("--downloads-path", default=DEFAULT_DOWNLOADS_PATH)
    common.add_argument("--include-bots", action="store_true")
    common.add_argument(
        "--no-fast-grep",
        action="store_true",
        help="scan full log without grep (slow on multi-GB files)",
    )

    ap = argparse.ArgumentParser(description="HackMe nginx downloads/ISO interest parser")
    sub = ap.add_subparsers(dest="cmd", required=True)

    r = sub.add_parser("report", parents=[common], help="scan log file")
    r.add_argument("--minutes", type=int, default=None, help="only last N minutes")
    r.add_argument("--since", default=None, help="YYYY-MM-DD")
    r.set_defaults(func=run_report, fast_grep=True)

    lv = sub.add_parser("live", parents=[common], help="tail -F and refresh dashboard")
    lv.add_argument("--window-minutes", type=int, default=60)
    lv.add_argument("--refresh-sec", type=float, default=2.0)
    lv.set_defaults(func=run_live, fast_grep=False)

    tl = sub.add_parser("tail", parents=[common], help="read log lines from stdin")
    tl.add_argument("--minutes", type=int, default=None)
    tl.set_defaults(func=run_tail, fast_grep=False)

    args = ap.parse_args()
    if getattr(args, "no_fast_grep", False):
        args.fast_grep = False
    elif args.cmd == "report":
        args.fast_grep = True
    else:
        args.fast_grep = False
    if args.client_log:
        args.log = args.client_log
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
