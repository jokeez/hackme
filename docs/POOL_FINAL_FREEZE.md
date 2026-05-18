# Pool release freeze — `1.0.0-pool`

Зафиксированное состояние **пулового продукта** (coordinator + worker + канонический оверлей + UI дашборда) для операторов и майнеров.

## Что входит в «финал пула»

- **Версия API-узла:** `version` в `GET /api/status` = `1.0.0-pool` (`main.go`).
- **Экономика:** консенсусный казначей, genesis mint 50 000 HMC на `DevFeeAddress`, `policy_hash` — см. `internal/chain/economics.go`, `internal/block/genesis.go`.
- **Майнинг:** `POST /api/worker/start`, coordinator (`cmd/coordinator`), подписанные submit-ы, rate limits — см. `cmd/coordinator/work.go`.
- **Сеть:** `HACKME_CANONICAL_CHAIN_URL`, `HACKME_POOL_COORDINATOR_URL`, опционально **`HACKME_PUBLIC_AUTHORITY_BASE`** (одна база command node → авто-заполнение канона и координатора при пустых env), опционально P2P + `network_sync` в `/api/status`, фоновый sync follower при `HACKME_P2P_BACKGROUND_SYNC_SEC` + `HACKME_P2P_SYNC_STATE_REPLAY_ENABLED`.
- **UI:** дашборд — полоса пула (canon Δ, P2P lag, ledger policy), бейджи public mining readiness, актуальный текст генезиса, шаг 3 = worker или leader PoH.

## Ограничения (честно)

- P2P apply **не** воспроизводит полное состояние аккаунтов; истина для кошелька в публичном режиме — **канонический HTTP**.
- HA coordinator / multi-master в коде **не** заявлены.

## Заморозка артефакта

Запуск (создаёт отчёт в `reports/pool-freeze-<timestamp>/`):

```bash
bash scripts/ops/pool_release_freeze.sh
```

Полный локальный чеклист без секретов (vet + тесты + audit + build; опционально публичные GET):

```bash
bash scripts/ops/repo_final_selfcheck.sh
# с проверкой публичного command node:
PUBLIC_RO_BASE=https://hackme.tech bash scripts/ops/repo_final_selfcheck.sh

# полный интеграционный дым (coordinator + нода + worker loop, ~1–2 мин):
RUN_LOCAL_STACK_SMOKE=1 bash scripts/ops/repo_final_selfcheck.sh
```

Вручную: сохранить бинарь, `go version`, вывод `GET /api/status` с прод-узла (без секретов), и тег git при наличии репозитория.

---

## Проверка репозитория (2026-05-12)

- `go build -trimpath` — OK  
- `go test ./... -count=1` — OK  
- `scripts/ops/pool_release_freeze.sh` — OK (артефакт в `reports/pool-freeze-*`)

Дальше обязательны только **операционные** шаги на вашей стороне (ниже).

---

## Финальный вердикт: что остаётся после кода

| Область | Статус в коде | Что остаётся вам |
|--------|----------------|------------------|
| Пул (coordinator, worker, подписи, лимиты) | Готово | Запуск процессов, `HACKME_*` env, токены |
| Канон + кошелёк в network mode | Готово | URL публичного command node, стабильный DNS/IP |
| Экономика / genesis / `policy_hash` | Зафиксировано | Новая цепь: чистая `data/`, один `POST /api/genesis`, один билд у всех |
| P2P sync (блоки) | Готово (опционально) | Включать state replay только осознанно; баланс — с канона |
| Полный replay SQLite state | **Не** в MVP | Не ждать из кода; опора на канонический API |
| HA, шардирование пула | **Не** в MVP | Один VPS или ручной failover |
| TLS, WAF, мониторинг | Не в бинаре | Nginx/Caddy, алерты, бэкапы `data/*.db` |
| Ключ казначея под `DevFeeAddress` (`HMC-719006d93916ad52`) | Не в git | Оператор: `go run ./tools/gen_treasury_key` при смене казны; сид только в `.secrets/` / vault, см. **`docs/TREASURY_KEY.md`** |

**Итог:** разработческий «минимум для пула» закрыт; открытый хвост — **инфраструктура, секреты, процесс запуска цепи и маркетинг**, а не недописанные фичи ядра.

---

## VPS: покупать сразу или сначала «выпуск»?

Рекомендуемый порядок (минимум риска и денег):

1. **Сначала зафиксировать релиз в артефакте** — уже есть `1.0.0-pool`, скрипт freeze, тесты. Это не требует VPS.
2. **Один недорогой VPS (staging)** — поднять тот же compose/systemd, проверить worker с 2–3 машин, nginx, токены. Параллельно правите сайт/маркетинг **на этом же URL или поддомене** (`pool-staging.…`).
3. **Прод-VPS за ~1 неделю до «публичного дня»** — новая цепь (как вы планировали), чистая БД, анонс genesis, при необходимости **другой** IP/имя для «боевого» command node, чтобы не путать со staging.

**Покупать «большой» прод-VPS до готовности маркетинга не обязательно:** можно вести разработку и интеграции на staging; прод оплатить, когда есть дата запуска и поток майнеров. Если **уже есть** рабочий публичный IP (как в README staging) — новый VPS нужен только когда решите отделить staging от prod или масштабировать.

**Кратко:** не блокируйте «выпуск» покупкой VPS; **блокируйте публичный прод-день** наличием стабильного прод-узла + новой цепи + готового анонса.

**Вердикты и границы публичного запуска (одним файлом):** см. **`docs/PUBLIC_LAUNCH_VERDICT.md`**. Быстрый автоматический срез: **`bash scripts/ops/public_release_readiness.sh`** (полный merge-уровень по-прежнему **`repo_final_selfcheck.sh`**).
