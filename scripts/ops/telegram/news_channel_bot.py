#!/usr/bin/env python3
"""
HackMe Channel News Bot (public / miners)

Reads news entries from news.json and publishes new items to a Telegram channel.
- Retries feed fetch; retries Telegram posts with fresh HTTP requests each attempt.
- Persists posted_ids after each successful send (crash-safe); optional ignored_ids for
  entries skipped by status (e.g. draft) so they are not retried forever.
- --dry-run never writes STATE_FILE (previously dry-run could incorrectly mark items posted).
- Second inline keyboard row links to Downloads / Economics / All news for miner UX.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import sys
import time
import html
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any, Dict, List, Tuple


def env_str(name: str, default: str = "") -> str:
    return str(os.getenv(name, default)).strip()


def env_int(name: str, default: int) -> int:
    raw = env_str(name, "")
    if not raw:
        return default
    try:
        return int(raw)
    except ValueError:
        return default


def env_float(name: str, default: float) -> float:
    raw = env_str(name, "")
    if not raw:
        return default
    try:
        return float(raw)
    except ValueError:
        return default


def env_truthy(name: str, default: bool) -> bool:
    raw = env_str(name, "")
    if raw == "":
        return default
    return raw.lower() in ("1", "true", "yes", "on")


def load_json_url(url: str, timeout_sec: int, max_retries: int) -> Dict[str, Any]:
    """Fetch JSON with small exponential backoff (CDN / TLS flakes)."""
    last_err: Exception | None = None
    fetch_url = url
    if "?" not in fetch_url:
        fetch_url = f"{fetch_url.rstrip('/')}?_ts={int(time.time())}"
    for attempt in range(1, max(1, max_retries) + 1):
        req = urllib.request.Request(
            fetch_url,
            headers={
                "User-Agent": "hackme-news-bot/2.2",
                "Accept": "application/json",
                "Accept-Encoding": "identity",
            },
        )
        try:
            with urllib.request.urlopen(req, timeout=timeout_sec) as resp:
                raw = resp.read()
                if len(raw) < 32:
                    raise ValueError(f"feed too short ({len(raw)} bytes)")
                return json.loads(raw.decode("utf-8"))
        except Exception as e:  # noqa: BLE001
            last_err = e
            if attempt < max_retries:
                time.sleep(min(8, 2**attempt))
    assert last_err is not None
    raise last_err


def tg_post(
    bot_token: str,
    payload: Dict[str, Any],
    timeout_sec: int,
    max_retries: int,
) -> Tuple[bool, str]:
    url = f"https://api.telegram.org/bot{bot_token}/sendMessage"
    data = json.dumps(payload).encode("utf-8")

    attempt = 0
    while attempt <= max_retries:
        attempt += 1
        # Fresh Request each attempt (some errors leave the connection in a bad state).
        req = urllib.request.Request(
            url,
            data=data,
            headers={"Content-Type": "application/json; charset=utf-8"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=timeout_sec) as resp:
                raw = resp.read().decode("utf-8")
                body = json.loads(raw)
                ok = bool(body.get("ok"))
                if ok:
                    return True, "ok"
                return False, f"telegram api error: {body}"
        except urllib.error.HTTPError as e:
            raw = ""
            try:
                raw = e.read().decode("utf-8")
            except Exception:
                pass
            retry_after = 0
            if raw:
                try:
                    body = json.loads(raw)
                    retry_after = int(body.get("parameters", {}).get("retry_after", 0))
                except Exception:
                    retry_after = 0
            if e.code == 429 and retry_after > 0 and attempt <= max_retries:
                time.sleep(retry_after)
                continue
            if e.code >= 500 and attempt <= max_retries:
                time.sleep(min(8, 2 * attempt))
                continue
            return False, f"http {e.code}: {raw[:400]}"
        except Exception as e:  # noqa: BLE001
            if attempt <= max_retries:
                time.sleep(min(8, 2 * attempt))
                continue
            return False, str(e)
    return False, "retry limit exceeded"


def state_load(path: Path) -> Dict[str, Any]:
    if not path.exists():
        return {"posted_ids": [], "ignored_ids": []}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except Exception:
        return {"posted_ids": [], "ignored_ids": []}
    posted = data.get("posted_ids")
    if not isinstance(posted, list):
        posted = []
    ign = data.get("ignored_ids")
    if not isinstance(ign, list):
        ign = []
    return {
        "posted_ids": [str(x) for x in posted],
        "ignored_ids": [str(x) for x in ign],
    }


def state_save(path: Path, state: Dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(state, ensure_ascii=True, indent=2) + "\n", encoding="utf-8")


def parse_blocked_statuses(raw: str) -> set[str]:
    """Status values we skip for Telegram (recorded in ignored_ids so we do not retry forever)."""
    s = (raw or "").strip().lower()
    if s in ("", "none", "off", "0", "-"):
        return set()
    parts = {p.strip().lower() for p in raw.split(",") if p.strip()}
    return parts


def parse_date(v: str) -> dt.datetime:
    s = (v or "").strip()
    if not s:
        return dt.datetime(1970, 1, 1, tzinfo=dt.timezone.utc)
    for fmt in ("%Y-%m-%d", "%Y-%m-%dT%H:%M:%SZ"):
        try:
            d = dt.datetime.strptime(s, fmt)
            return d.replace(tzinfo=dt.timezone.utc)
        except ValueError:
            pass
    return dt.datetime(1970, 1, 1, tzinfo=dt.timezone.utc)


def site_home_url(news_page_base: str, override: str) -> str:
    o = (override or "").strip()
    if o:
        return o.rstrip("/")
    u = urllib.parse.urlparse(news_page_base)
    if u.scheme and u.netloc:
        return f"{u.scheme}://{u.netloc}".rstrip("/")
    return "https://hackme.tech"


def render_message(item: Dict[str, Any], max_len: int = 3900, *, miner_hint: bool = True) -> str:
    title = html.escape(str(item.get("title", "HackMe Update")))
    date = html.escape(str(item.get("date", "")))
    status = html.escape(str(item.get("status", "update")))
    summary = html.escape(str(item.get("summary", "")))
    impact = html.escape(str(item.get("impact", "")))
    action = html.escape(str(item.get("action", "")))
    tags = item.get("tags", [])
    tags_text = " ".join(f"#{html.escape(str(t)).replace(' ', '_')}" for t in tags if str(t).strip())

    # Keep channel posts readable; long "action" lines break mobile Telegram.
    def cap(s: str, n: int) -> str:
        if len(s) <= n:
            return s
        return s[: max(0, n - 1)] + "…"

    summary = cap(summary, 2200)
    impact = cap(impact, 900)
    action = cap(action, 900)

    lines = [
        "<b>HackMe Network Update</b>",
        "",
        f"<b>{title}</b>",
        f"Date: {date}",
        f"Status: <code>{status}</code>",
        "",
        f"<b>Summary:</b> {summary}" if summary else "",
        f"<b>Impact:</b> {impact}" if impact else "",
        f"<b>Action:</b> {action}" if action else "",
        f"<b>Tags:</b> {tags_text}" if tags_text else "",
    ]
    if miner_hint:
        lines.extend(["", "<i>Miners: bundles &amp; pool economics on the site — use the buttons below.</i>"])
    out = "\n".join(line for line in lines if line != "")
    if len(out) <= max_len:
        return out
    return out[: max_len - 60] + "\n\n<i>(truncated for Telegram)</i>"


def build_button_url(base: str, item_id: str) -> str:
    b = base.rstrip("#")
    if "#" in b:
        return f"{b}{item_id}"
    return f"{b}#{urllib.parse.quote(item_id)}"


def build_reply_markup(
    btn_url: str,
    *,
    site_home: str,
    extra_row: bool,
    item_links: Dict[str, Any] | None = None,
) -> Dict[str, Any]:
    rows: List[List[Dict[str, str]]] = [
        [{"text": "Read full update", "url": btn_url}],
    ]
    if extra_row and site_home:
        h = site_home.rstrip("/")
        rows.append(
            [
                {"text": "Downloads", "url": f"{h}/downloads.html"},
                {"text": "Economics", "url": f"{h}/economics-model.html"},
                {"text": "All news", "url": f"{h}/news.html"},
            ]
        )
    if isinstance(item_links, dict):
        link_row: List[Dict[str, str]] = []
        for key, label in (
            ("github", "GitHub ⭐"),
            ("bitcointalk", "Bitcointalk"),
            ("telegram", "Telegram"),
        ):
            u = str(item_links.get(key, "")).strip()
            if u:
                link_row.append({"text": label, "url": u})
        if link_row:
            rows.append(link_row[:3])
    return {"inline_keyboard": rows}


def run_once(
    *,
    bot_token: str,
    chat_id: str,
    news_url: str,
    news_page_base: str,
    state_path: Path,
    timeout_sec: int,
    fetch_retries: int,
    max_retries: int,
    dry_run: bool,
    blocked_statuses: set[str],
    site_home: str,
    miner_button_row: bool,
    post_gap_sec: float,
    max_text: int,
    miner_hint: bool,
) -> int:
    try:
        payload = load_json_url(news_url, timeout_sec, fetch_retries)
    except Exception as e:  # noqa: BLE001
        print(f"[news-bot] failed to load news feed: {e}", file=sys.stderr)
        return 1

    items = payload.get("items", [])
    if not isinstance(items, list):
        print("[news-bot] invalid feed: items must be a list", file=sys.stderr)
        return 1
    normalized: List[Dict[str, Any]] = []
    for it in items:
        if not isinstance(it, dict):
            continue
        item_id = str(it.get("id", "")).strip()
        if not item_id:
            continue
        it["id"] = item_id
        normalized.append(it)
    normalized.sort(key=lambda x: parse_date(str(x.get("date", ""))))

    state = state_load(state_path)
    posted_list = list(state.get("posted_ids", []))
    ignored_list = list(state.get("ignored_ids", []))
    posted_set = set(posted_list)
    ignored_set = set(ignored_list)

    candidates = [
        it
        for it in normalized
        if it["id"] not in posted_set and it["id"] not in ignored_set
    ]
    if not candidates:
        print("[news-bot] no new items")
        return 0

    work_queue: List[Dict[str, Any]] = []
    for it in candidates:
        st = str(it.get("status", "")).strip().lower()
        if st and st in blocked_statuses:
            if dry_run:
                print(f"[news-bot] DRY_RUN skip blocked status={st!r} id={it['id']}")
            else:
                ignored_list.append(it["id"])
                ignored_set.add(it["id"])
                if len(ignored_list) > 2000:
                    ignored_list = ignored_list[-2000:]
                state_save(
                    state_path,
                    {"posted_ids": posted_list, "ignored_ids": ignored_list},
                )
                print(f"[news-bot] ignored (status={st}) id={it['id']}")
            continue
        work_queue.append(it)

    if not work_queue:
        print("[news-bot] no postable items after status filter")
        return 0

    print(f"[news-bot] postable new items: {len(work_queue)}")
    for it in work_queue:
        msg = render_message(it, max_len=max_text, miner_hint=miner_hint)
        btn_url = build_button_url(news_page_base, it["id"])
        tg_payload: Dict[str, Any] = {
            "chat_id": chat_id,
            "text": msg,
            "parse_mode": "HTML",
            "disable_web_page_preview": True,
            "reply_markup": build_reply_markup(
                btn_url,
                site_home=site_home,
                extra_row=miner_button_row,
                item_links=it.get("links") if isinstance(it.get("links"), dict) else None,
            ),
        }

        if dry_run:
            print(f"[news-bot] DRY_RUN would post id={it['id']}")
            print(msg)
            continue

        ok, detail = tg_post(bot_token, tg_payload, timeout_sec, max_retries)
        if not ok:
            print(f"[news-bot] post failed for {it['id']}: {detail}", file=sys.stderr)
            return 2
        print(f"[news-bot] posted {it['id']}")
        posted_list.append(it["id"])
        posted_set.add(it["id"])
        if len(posted_list) > 500:
            posted_list = posted_list[-500:]
        state_save(
            state_path,
            {"posted_ids": posted_list, "ignored_ids": ignored_list},
        )
        time.sleep(post_gap_sec)

    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="HackMe Telegram channel news bot")
    parser.add_argument("--once", action="store_true", help="run a single polling cycle")
    parser.add_argument("--dry-run", action="store_true", help="render messages without sending")
    args = parser.parse_args()

    bot_token = env_str("TG_BOT_TOKEN")
    chat_id = env_str("TG_CHAT_ID")
    news_url = env_str("NEWS_FEED_URL", "https://hackme.tech/assets/news.json")
    news_page_base = env_str("NEWS_PAGE_BASE", "https://hackme.tech/news.html#")
    interval_sec = env_int("POLL_INTERVAL_SEC", 60)
    timeout_sec = env_int("HTTP_TIMEOUT_SEC", 20)
    fetch_retries = env_int("HTTP_FETCH_RETRIES", 3)
    max_retries = env_int("POST_RETRIES", 4)
    state_file = env_str("STATE_FILE", "/opt/hackme/data/news-bot-state.json")
    state_path = Path(state_file)
    blocked_statuses = parse_blocked_statuses(env_str("NEWS_BLOCKED_STATUSES", "draft"))
    site_home = site_home_url(news_page_base, env_str("NEWS_SITE_HOME"))
    miner_button_row = env_truthy("NEWS_MINER_BUTTON_ROW", True)
    miner_hint = env_truthy("NEWS_MINER_HINT_LINE", True)
    post_gap_sec = env_float("POST_GAP_SEC", 1.1)
    if post_gap_sec < 0.2:
        post_gap_sec = 0.2
    if post_gap_sec > 30:
        post_gap_sec = 30.0
    max_text = env_int("TELEGRAM_MAX_TEXT", 3900)
    if max_text < 800:
        max_text = 800
    if max_text > 4000:
        max_text = 4000

    if not args.dry_run:
        if not bot_token:
            print("[news-bot] TG_BOT_TOKEN is required", file=sys.stderr)
            return 3
        if not chat_id:
            print("[news-bot] TG_CHAT_ID is required", file=sys.stderr)
            return 3

    if interval_sec < 15:
        interval_sec = 15

    if args.once:
        return run_once(
            bot_token=bot_token,
            chat_id=chat_id,
            news_url=news_url,
            news_page_base=news_page_base,
            state_path=state_path,
            timeout_sec=timeout_sec,
            fetch_retries=fetch_retries,
            max_retries=max_retries,
            dry_run=args.dry_run,
            blocked_statuses=blocked_statuses,
            site_home=site_home,
            miner_button_row=miner_button_row,
            post_gap_sec=post_gap_sec,
            max_text=max_text,
            miner_hint=miner_hint,
        )

    print(
        f"[news-bot] start polling interval={interval_sec}s feed={news_url} chat={chat_id or 'dry-run'}"
    )
    while True:
        code = run_once(
            bot_token=bot_token,
            chat_id=chat_id,
            news_url=news_url,
            news_page_base=news_page_base,
            state_path=state_path,
            timeout_sec=timeout_sec,
            fetch_retries=fetch_retries,
            max_retries=max_retries,
            dry_run=args.dry_run,
            blocked_statuses=blocked_statuses,
            site_home=site_home,
            miner_button_row=miner_button_row,
            post_gap_sec=post_gap_sec,
            max_text=max_text,
            miner_hint=miner_hint,
        )
        if code != 0:
            print(f"[news-bot] cycle ended with code={code}", file=sys.stderr)
        time.sleep(interval_sec)


if __name__ == "__main__":
    raise SystemExit(main())
