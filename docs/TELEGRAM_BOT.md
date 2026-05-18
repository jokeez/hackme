# Telegram-бот для мониторинга HackMe

Каждый оператор запускает бота **у себя**: свой токен от [@BotFather](https://t.me/BotFather), свой URL узла HackMe, опционально — белый список Telegram user id.

## Быстрый старт

1. В корне репозитория (рядом с `go.mod`):

   ```bash
   cp scripts/ops/telegram_bot.env.example .env.telegram
   ```

2. Отредактируйте `.env.telegram`:
   - **`TELEGRAM_BOT_TOKEN`** — токен бота (не публикуйте и не коммитьте).
   - **`HACKME_TELEGRAM_NODE_URL`** — адрес вашего узла, например `http://127.0.0.1:8080` или `https://ваш-домен:порт`.
   - **`HACKME_TELEGRAM_ALLOWED_USER_IDS`** — через запятую числовые id пользователей Telegram, которым разрешено пользоваться ботом. **Рекомендуется**, если бот доступен из интернета. Узнать свой id: [@userinfobot](https://t.me/userinfobot).

3. Запуск:

   ```bash
   go run ./cmd/telegrambot
   ```

   Либо скрипт из корня репозитория:

   ```bash
   bash scripts/ops/telegram_bot_up.sh
   ```

## Явный файл конфигурации

Переменные из оболочки **имеют приоритет** над строками в файле.

```bash
go run ./cmd/telegrambot -config /home/you/operator.env
```

Или задайте путь через окружение (без `-config`):

```bash
export HACKME_TELEGRAM_CONFIG=/home/you/operator.env
go run ./cmd/telegrambot
```

Если ни `-config`, ни `HACKME_TELEGRAM_CONFIG` не заданы, бот подхватывает **первый существующий** из файлов в текущей директории: `.env.telegram`, затем `telegram_bot.env`.

## Справка по флагам и переменным

```bash
go run ./cmd/telegrambot -help
```

## Команды в Telegram

`/digest`, `/status`, `/metrics`, `/wallet`, `/worker`, `/blocks`, `/watch`, `/unwatch`, `/about`, `/help` — см. ответ бота на `/start`.

## Сборка бинарника (VPS без исходников в PATH)

```bash
go build -o hackme-telegrambot ./cmd/telegrambot
./hackme-telegrambot
```

Запускайте из каталога, где лежит `.env.telegram`, или всегда передавайте `-config`.

## systemd (пример)

Подставьте пользователя и пути:

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

Узел HackMe (`go run .`) и бот — **разные процессы**; URL узла задаётся в `HACKME_TELEGRAM_NODE_URL`.

## Безопасность

- Токен бота = полный контроль над ботом; храните только в env / `EnvironmentFile`, не в git.
- Файлы `.env.telegram` и `telegram_bot.env` перечислены в `.gitignore`.
- Для публичного бота обязательно **`HACKME_TELEGRAM_ALLOWED_USER_IDS`**.

## Второй бот — канал для майнеров и сообщества

Отдельный процесс: **`scripts/ops/telegram/news_channel_bot.py`** публикует записи из **`assets/news.json`** в публичный Telegram-канал (нужны `TG_BOT_TOKEN`, `TG_CHAT_ID`). Не путать с операторским ботом выше: у канал-бота **нет** привязки к вашему узлу, только к ленте новостей и ссылкам на сайт.

Полный чеклист: **`docs/TELEGRAM_NEWS_BOT_RUNBOOK.md`** (systemd `hackme-news-bot.service`, dry-run, ротация токена, watchdog).
