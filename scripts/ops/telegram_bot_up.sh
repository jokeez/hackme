#!/usr/bin/env bash
# Launch a Telegram bot from the root of the HackMe repository.
# Each operator puts its own .env.telegram (see scripts/ops/telegram_bot.env.example and docs/TELEGRAM_BOT.md).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
exec go run ./cmd/telegrambot "$@"
