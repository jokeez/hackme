# Telegram bot for monitoring HackMe

Each operator launches a bot **at home**: its own token from [@BotFather](https://t.me/BotFather), its own HackMe node URL, and optionally a whitelist of Telegram user id.

## Quick start

1. At the root of the repository (next to `go.mod`):

   ```bash
   cp scripts/ops/telegram_bot.env.example .env.telegram
   ```

2. Edit `.env.telegram`:
   - **`TELEGRAM_BOT_TOKEN`** - bot token (do not publish or commit).
   - **`HACKME_TELEGRAM_NODE_URL`** is the address of your node, for example `http://127.0.0.1:8080` or `https://your-domain:port`.
   - **`HACKME_TELEGRAM_ALLOWED_USER_IDS`** — comma-separated numeric ids of Telegram users who are allowed to use the bot. **Recommended** if the bot is accessible from the Internet. Find out your id: [@userinfobot](https://t.me/userinfobot).

3. Launch:

   ```bash
   go run ./cmd/telegrambot
   ```

Or a script from the root of the repository:

   ```bash
   bash scripts/ops/telegram_bot_up.sh
   ```

## Explicit configuration file

Variables from the shell **take precedence** over lines in the file.

```bash
go run ./cmd/telegrambot -config /home/you/operator.env
```

Or specify a path through the environment (without `-config`):

```bash
export HACKME_TELEGRAM_CONFIG=/home/you/operator.env
go run ./cmd/telegrambot
```

If neither `-config` nor `HACKME_TELEGRAM_CONFIG` are specified, the bot picks up the **first existing** file in the current directory: `.env.telegram`, then `telegram_bot.env`.

## Help on flags and variables

```bash
go run ./cmd/telegrambot -help
```

## Teams in Telegram

| Team | What shows |
|---------|----------------|
| `/digest` | Summary: height, pool GH/s, balance, mining |
| `/status` | Chain tip, genesis, mining flag |
| `/metrics` | Local PoH: target_mod, attempts/s, task kind, reward |
| `/pool` | **Pool:** hashrate, active rigs, coordinator counters |
| `/tasks` | **Orders/fuzzing:** open/completed, reward, progress |
| `/blocks [n]` | Latest blocks (task kind, hash) |
| `/wallet` | Node Wallet Balance |
| `/worker` | Local worker + unpaid accrual |
| `/watch` | Alert for new `tip_height` |
| `/unwatch` | Turn off alerts |

The buttons below the messages duplicate the commands (↻ = update).

## Two different bots - do not confuse them

| Bot | Process | Destination |
|-----|---------|------------|
| **Operator** (`cmd/telegrambot`) | `hackme-telegrambot.service` | **You** in PM: hashrate, blocks, pool, tasks |
| **News channel** (`news_channel_bot.py`) | `hackme-news-bot.service` | Autopost news to the channel @hackme_tech |

The operator bot needs a **separate** token from [@BotFather](https://t.me/BotFather) (not the same as the channel news bot, if you don’t want to mix roles).

## Deploy to VPS (hackme.tech) - option A

One operator bot on the product node (`127.0.0.1:18080`), a separate token from the news channel.

1. [@BotFather](https://t.me/BotFather) → **New bot** (not the same as for @hackme_tech).
2. Locally (file in `.gitignore`):

   ```bash
   echo '123456:ABC...' > .secrets/telegram_operator_bot_token
   chmod 600 .secrets/telegram_operator_bot_token
   ```

3. Optional - only your Telegram id ([@userinfobot](https://t.me/userinfobot)):

   ```bash
   export HACKME_TELEGRAM_ALLOWED_USER_IDS=123456789
   ```

4. From a machine with SSH to VPS:

   ```bash
   NODE_SSH=hackme-vps bash scripts/ops/setup_telegram_operator_bot.sh
   ```

The script collects `hackme-telegrambot`, copies `HACKME_ADMIN_TOKEN` from `/opt/hackme/.env.vps`, includes `hackme-telegrambot.service` (log: `/opt/hackme/logs/telegram-operator-bot.log`).

Without a local file - on VPS:

```bash
echo 'TOKEN' | sudo tee /opt/hackme/.secrets/telegram_operator_bot_token
sudo chmod 600 /opt/hackme/.secrets/telegram_operator_bot_token
sudo chown hackme:hackme /opt/hackme/.secrets/telegram_operator_bot_token
NODE_SSH=hackme-vps bash scripts/ops/setup_telegram_operator_bot.sh
```

Find out your Telegram id: [@userinfobot](https://t.me/userinfobot).

Old list: `/digest`, `/status`, `/metrics`, `/wallet`, `/worker`, `/blocks`, `/watch`, `/unwatch`, `/about`, `/help` - see answer bot on `/start`.

## Build binary (VPS without sources in PATH)

```bash
go build -o hackme-telegrambot ./cmd/telegrambot
./hackme-telegrambot
```

Run from the directory where `.env.telegram` is, or always pass `-config`.

## systemd (example)

Substitute user and paths:

```ini
[Unit]
Description=HackMe Telegram operator bot
After=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/hackme
EnvironmentFile=/opt/hackme/.env.telegram
ExecStart=/opt/hackme/hackme-telegrambot
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

HackMe node (`go run .`) and bot are **different processes**; The node URL is specified in `HACKME_TELEGRAM_NODE_URL`.

## Safety

- Bot token = full control over the bot; store only in env/`EnvironmentFile`, not in git.
- Files `.env.telegram` and `telegram_bot.env` are listed under `.gitignore`.
- For a public bot, **`HACKME_TELEGRAM_ALLOWED_USER_IDS`** is required.

## The second bot is a channel for miners and the community

Separate process: **`scripts/ops/telegram/news_channel_bot.py`** publishes posts from **`assets/news.json`** to a public Telegram channel (needs `TG_BOT_TOKEN`, `TG_CHAT_ID`). Not to be confused with the operator bot above: the channel bot has **no** connection to your node, only to the news feed and links to the site. Optional systemd unit: `hackme-news-bot.service`.
