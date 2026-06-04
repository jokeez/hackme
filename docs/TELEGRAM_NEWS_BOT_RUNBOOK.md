# Telegram News Bot Runbook

This runbook covers setup, monitoring, token rotation, and dry-run checks for the official HackMe channel news bot.

## Components

- Bot script: `scripts/ops/telegram/news_channel_bot.py`
- Env file (VPS): `/opt/hackme/.env.newsbot`
- Service unit: `scripts/ops/systemd/hackme-news-bot.service`
- Watchdog:
  - `scripts/ops/news_bot_healthcheck.sh`
  - `scripts/ops/systemd/hackme-news-bot-watchdog.service`
  - `scripts/ops/systemd/hackme-news-bot-watchdog.timer`

## Initial Setup (VPS)

1. Create bot in `@BotFather`, add bot as channel admin (Post Messages permission).
2. Create env from template:

```bash
cp /opt/hackme/scripts/ops/telegram/newsbot.env.example /opt/hackme/.env.newsbot
chmod 600 /opt/hackme/.env.newsbot
```

3. Set real values in `/opt/hackme/.env.newsbot` (include admin chat for pool heartbeat).

From your workstation (chat id not stored in git):

```bash
TG_ADMIN_CHAT_ID=<your_numeric_chat_id> NODE_SSH=hackme-vps bash scripts/ops/vps_set_telegram_admin_chat.sh
```

Or edit on VPS:

```bash
TG_ADMIN_CHAT_ID=-1001234567890   # operator group (numeric id)
POOL_HEARTBEAT_INTERVAL_SEC=14400 # 4 hours
POOL_STATUS_URL=https://hackme.tech/api/status?lite=1
POOL_WORK_STATS_URL=https://hackme.tech/pool/coordinator/api/work/stats?details=0
```

Dry-run heartbeat:

```bash
TG_ADMIN_CHAT_ID=... TG_BOT_TOKEN=... python3 scripts/ops/telegram/news_channel_bot.py --heartbeat-once --dry-run
```

4. Set `TG_BOT_TOKEN`, `TG_CHAT_ID` (e.g. `@hackme_tech`), and optional `TG_ADMIN_CHAT_ID` for heartbeat.

5. Install/start service:

```bash
sudo bash /opt/hackme/scripts/ops/setup_news_bot_service.sh
```

## Dry-run (no Telegram send)

`--dry-run` **does not write** `STATE_FILE` (it only prints what would be posted). Use a throwaway `STATE_FILE` if you want to test against a copy of production state.

```bash
NEWS_FEED_URL=https://hackme.tech/assets/news.json \
STATE_FILE=/tmp/hackme-newsbot-state.json \
python3 /opt/hackme/scripts/ops/telegram/news_channel_bot.py --once --dry-run
```

## State file (`news-bot-state.json`)

- **`posted_ids`**: news item ids successfully sent to Telegram (bounded to last 500).
- **`ignored_ids`**: ids skipped because `status` matched `NEWS_BLOCKED_STATUSES` (default `draft`), so the bot does not retry them every poll. Clear manually if you later publish that item under a new id.

## Channel post format (v3)

- **English only** in `telegram` blocks (headline, lead, bullets, footer). Site `summary` can stay technical English too.
- **No** `Status:` / `Tags:` labels. Hashtags from `tags` in JSON appear at the bottom as `#release #mining` (no “Tags:” line).
- **No** raw `Action:` URL dumps in the message body.
- **No** inline button linking to `t.me/…` (you are already in Telegram).
- Optional per-item copy in `news.json`:

```json
"tags": ["release", "mining"],
"telegram": {
  "headline": "Short title",
  "lead": "One opening sentence.",
  "bullets": ["Point one", "Point two"],
  "footer": "Optional closing hint."
}
```

Preview locally:

```bash
bash scripts/ops/telegram/preview_channel_post.sh 2026-06-04-rc11l-iso-live-boot
```

Buttons (default): **Read on site** · **Downloads** · **Pool stats** · **Economics**. Set `NEWS_SHOW_GITHUB_BUTTON=1` to add GitHub. Disable rows with `NEWS_MINER_BUTTON_ROW=0` / `NEWS_MINER_HINT_LINE=0`.

Verdicts for GitHub operators: [verdicts/INDEX.md](verdicts/INDEX.md) — not `docs/TELEGRAM_POST_*` drafts.

## Operational Monitoring

Service status:

```bash
systemctl is-active hackme-news-bot.service
systemctl --no-pager --full status hackme-news-bot.service
```

Logs:

```bash
journalctl -u hackme-news-bot.service -n 100 --no-pager
journalctl -u hackme-news-bot.service -f
tail -n 120 /opt/hackme/logs/news-bot.log
```

State file:

```bash
python3 - <<'PY'
import json, pathlib
p=pathlib.Path('/opt/hackme/data/news-bot-state.json')
print('exists:', p.exists())
if p.exists():
    d=json.loads(p.read_text())
    ids=d.get('posted_ids',[])
    ign=d.get('ignored_ids',[])
    print('posted_count:', len(ids))
    print('ignored_count:', len(ign))
    print('last_posted:', ids[-1] if ids else None)
    print('last_ignored:', ign[-1] if ign else None)
PY
```

## Watchdog Setup (recommended)

Install watchdog units:

```bash
sudo cp /opt/hackme/scripts/ops/systemd/hackme-news-bot-watchdog.service /etc/systemd/system/
sudo cp /opt/hackme/scripts/ops/systemd/hackme-news-bot-watchdog.timer /etc/systemd/system/
sudo chmod +x /opt/hackme/scripts/ops/news_bot_healthcheck.sh
sudo systemctl daemon-reload
sudo systemctl enable --now hackme-news-bot-watchdog.timer
systemctl list-timers --all | grep hackme-news-bot-watchdog || true
```

Watchdog log:

```bash
tail -n 120 /opt/hackme/logs/news-bot-watchdog.log
```

## Token Rotation (mandatory if leaked)

1. In `@BotFather` revoke/regenerate token.
2. Update `TG_BOT_TOKEN` in `/opt/hackme/.env.newsbot`.
3. Restart service:

```bash
systemctl restart hackme-news-bot.service
```

4. Verify with status/log checks above.

## Changing Channel

Update `TG_CHAT_ID` in `/opt/hackme/.env.newsbot`:

- public channel username (preferred): `@your_channel`
- or numeric chat id

Restart service:

```bash
systemctl restart hackme-news-bot.service
```

## Validation Checklist (green)

- Service active.
- No parse errors in logs.
- New item in `news.json` results in one Telegram post.
- Re-running bot does **not** duplicate already posted IDs.
- `news-bot-state.json` updates with latest item ID.
