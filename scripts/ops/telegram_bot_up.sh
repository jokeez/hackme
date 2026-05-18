#!/usr/bin/env bash
# Запуск Telegram-бота из корня репозитория HackMe.
# Каждый оператор кладёт свой .env.telegram (см. scripts/ops/telegram_bot.env.example и docs/TELEGRAM_BOT.md).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
exec go run ./cmd/telegrambot "$@"
