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

3. Set real values in `/opt/hackme/.env.newsbot`:

- `TG_BOT_TOKEN`
- `TG_CHAT_ID` (e.g. `@hackme_tech`)

4. Install/start service:

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

## Miner-facing extras (defaults on)

The bot adds a short miner hint line and a second inline row: **Downloads**, **Economics**, **All news** (URLs from `NEWS_SITE_HOME` or derived from `NEWS_PAGE_BASE`). Disable with `NEWS_MINER_BUTTON_ROW=0` and/or `NEWS_MINER_HINT_LINE=0` in `.env.newsbot`.

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
